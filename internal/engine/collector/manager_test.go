package collector

import (
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestManagerStatusRecoversAfterSuccessfulRead(t *testing.T) {
	m := NewManager(nil, nil)
	id := uuid.New()

	m.MarkConnected(id)
	online, offline := m.Times(id)
	if m.Status(id) != "connected" || online == nil || offline != nil {
		t.Fatalf("expected connected status with online timestamp, got status=%q online=%v offline=%v", m.Status(id), online, offline)
	}
	firstOnline := *online

	m.MarkDisconnected(id)
	if m.Status(id) != "disconnected" {
		t.Fatalf("expected disconnected status, got %q", m.Status(id))
	}
	_, offline = m.Times(id)
	if offline == nil {
		t.Fatal("expected offline timestamp after disconnect")
	}

	time.Sleep(2 * time.Millisecond)
	m.MarkConnected(id)
	online, offline = m.Times(id)
	if m.Status(id) != "connected" || online == nil || offline == nil {
		t.Fatalf("expected recovered connected status, got status=%q online=%v offline=%v", m.Status(id), online, offline)
	}
	if !online.After(firstOnline) {
		t.Fatalf("expected reconnect to refresh online timestamp: first=%v current=%v", firstOnline, *online)
	}
}

func TestManagerRepeatedConnectedDoesNotRefreshOnlineTimestamp(t *testing.T) {
	m := NewManager(nil, nil)
	id := uuid.New()
	m.MarkConnected(id)
	first, _ := m.Times(id)
	m.MarkConnected(id)
	second, _ := m.Times(id)
	if first == nil || second == nil || !first.Equal(*second) {
		t.Fatalf("repeated connected mark changed online timestamp: first=%v second=%v", first, second)
	}
}
