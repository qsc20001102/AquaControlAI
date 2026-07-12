package collector

import (
	"aquacontrolai/internal/protocol"
	pg "aquacontrolai/internal/repository/postgres"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"sync"
	"time"
)

type Manager struct {
	mu          sync.RWMutex
	connections map[uuid.UUID]protocol.Connection
	statuses    map[uuid.UUID]string
	lastOnline  map[uuid.UUID]time.Time
	lastOffline map[uuid.UUID]time.Time
	store       *pg.Store
	registry    *protocol.Registry
}

func NewManager(store *pg.Store, registry *protocol.Registry) *Manager {
	return &Manager{connections: map[uuid.UUID]protocol.Connection{}, statuses: map[uuid.UUID]string{}, lastOnline: map[uuid.UUID]time.Time{}, lastOffline: map[uuid.UUID]time.Time{}, store: store, registry: registry}
}
func (m *Manager) Connection(ctx context.Context, id uuid.UUID) (protocol.Connection, error) {
	m.mu.RLock()
	c := m.connections[id]
	m.mu.RUnlock()
	if c != nil {
		return c, nil
	}
	d, e := m.store.GetDevice(ctx, id)
	if e != nil {
		return nil, e
	}
	if !d.Enabled {
		m.setStatus(id, "disabled")
		return nil, context.Canceled
	}
	f, e := m.registry.Get(d.ProtocolType)
	if e != nil {
		return nil, e
	}
	var cfg map[string]any
	if e = json.Unmarshal(d.ProtocolConfig, &cfg); e != nil {
		return nil, e
	}
	connectCtx, cancel := context.WithTimeout(ctx, time.Duration(d.ConnectTimeout)*time.Second)
	defer cancel()
	c, e = f.NewConnection(connectCtx, protocol.DeviceConnectionConfig{DeviceID: id, Host: d.Host, Port: d.Port, ConnectTimeoutSeconds: d.ConnectTimeout, ProtocolConfig: cfg})
	if e != nil {
		m.setStatus(id, "disconnected")
		m.markOffline(id)
		return nil, e
	}
	m.mu.Lock()
	if existing := m.connections[id]; existing != nil {
		m.mu.Unlock()
		_ = c.Close()
		return existing, nil
	}
	m.connections[id] = c
	m.statuses[id] = "connected"
	m.lastOnline[id] = time.Now()
	m.mu.Unlock()
	return c, nil
}
func (m *Manager) Invalidate(id uuid.UUID) {
	m.mu.Lock()
	c := m.connections[id]
	delete(m.connections, id)
	m.statuses[id] = "disconnected"
	m.lastOffline[id] = time.Now()
	m.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
}
func (m *Manager) Status(id uuid.UUID) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s := m.statuses[id]; s != "" {
		return s
	}
	return "disconnected"
}
func (m *Manager) Times(id uuid.UUID) (online, offline *time.Time) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if t, ok := m.lastOnline[id]; ok {
		v := t
		online = &v
	}
	if t, ok := m.lastOffline[id]; ok {
		v := t
		offline = &v
	}
	return
}
func (m *Manager) MarkDisconnected(id uuid.UUID) {
	m.mu.Lock()
	m.statuses[id] = "disconnected"
	m.lastOffline[id] = time.Now()
	m.mu.Unlock()
}
func (m *Manager) setStatus(id uuid.UUID, s string) {
	m.mu.Lock()
	m.statuses[id] = s
	if s == "disconnected" {
		m.lastOffline[id] = time.Now()
	}
	m.mu.Unlock()
}
func (m *Manager) markOffline(id uuid.UUID) {
	m.mu.Lock()
	m.lastOffline[id] = time.Now()
	m.mu.Unlock()
}
func (m *Manager) Close() {
	m.mu.Lock()
	all := m.connections
	m.connections = map[uuid.UUID]protocol.Connection{}
	m.mu.Unlock()
	for _, c := range all {
		_ = c.Close()
	}
}
