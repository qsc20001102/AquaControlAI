package collector

import (
	"aquacontrolai/internal/protocol"
	pg "aquacontrolai/internal/repository/postgres"
	td "aquacontrolai/internal/repository/tdengine"
	"context"
	"github.com/google/uuid"
	"log/slog"
	"sync"
	"time"
)

type LatestValue struct {
	Value         *float64  `json:"value"`
	Quality       string    `json:"quality"`
	QualityReason *string   `json:"quality_reason"`
	TS            time.Time `json:"ts"`
}
type Engine struct {
	manager     *Manager
	pg          *pg.Store
	td          *td.Store
	workers     int
	jobs        chan pg.PointRow
	mu          sync.RWMutex
	latest      map[uuid.UUID]LatestValue
	lastHistory map[uuid.UUID]time.Time
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

func NewEngine(manager *Manager, pgStore *pg.Store, tdStore *td.Store, workers int) *Engine {
	if workers < 1 {
		workers = 1
	}
	return &Engine{manager: manager, pg: pgStore, td: tdStore, workers: workers, jobs: make(chan pg.PointRow, workers*4), latest: map[uuid.UUID]LatestValue{}, lastHistory: map[uuid.UUID]time.Time{}}
}
func (e *Engine) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	e.cancel = cancel
	for i := 0; i < e.workers; i++ {
		e.wg.Add(1)
		go e.worker(ctx)
	}
	e.wg.Add(1)
	go e.schedule(ctx)
	slog.Info("采集引擎已启动", "workers", e.workers)
}
func (e *Engine) Stop() {
	if e.cancel != nil {
		e.cancel()
	}
	e.wg.Wait()
	slog.Info("采集引擎已停止")
}
func (e *Engine) Latest(id uuid.UUID) *LatestValue {
	e.mu.RLock()
	defer e.mu.RUnlock()
	v, ok := e.latest[id]
	if !ok {
		return nil
	}
	return &v
}
func (e *Engine) schedule(ctx context.Context) {
	defer e.wg.Done()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	last := map[uuid.UUID]time.Time{}
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			points, err := e.pg.ListPoints(ctx, "collection", "", false)
			if err != nil {
				slog.Error("加载采集点失败", "error", err)
				continue
			}
			for _, p := range points {
				if !p.Enabled {
					continue
				}
				if now.Sub(last[p.ID]) < time.Duration(p.CollectInterval)*time.Second {
					continue
				}
				select {
				case e.jobs <- p:
					last[p.ID] = now
				default:
					slog.Warn("采集任务队列已满", "point_id", p.ID)
				}
			}
		}
	}
}
func (e *Engine) worker(ctx context.Context) {
	defer e.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case p := <-e.jobs:
			e.collect(ctx, p)
		}
	}
}
func (e *Engine) collect(ctx context.Context, p pg.PointRow) {
	now := time.Now()
	var value *float64
	quality := 1
	reasonText := "disconnected"
	reason := &reasonText
	c, err := e.manager.Connection(ctx, p.DeviceID)
	if err == nil {
		v, readErr := c.Read(ctx, p.Address, protocol.DataType(p.DataType))
		if readErr == nil {
			value = &v
			quality = 0
			reason = nil
		} else {
			reasonText = "read_error"
			reason = &reasonText
			e.manager.MarkDisconnected(p.DeviceID)
		}
	}
	q := "good"
	if quality == 1 {
		q = "bad"
	}
	e.mu.Lock()
	e.latest[p.ID] = LatestValue{value, q, reason, now}
	last := e.lastHistory[p.ID]
	shouldStore := p.StoreHistory && now.Sub(last) >= time.Duration(p.HistoryInterval)*time.Minute
	if shouldStore {
		e.lastHistory[p.ID] = now
	}
	e.mu.Unlock()
	if shouldStore {
		d, deviceErr := e.pg.GetDevice(ctx, p.DeviceID)
		if deviceErr == nil {
			if markErr := e.pg.MarkHistoryStarted(ctx, p.ID); markErr == nil {
				if insertErr := e.td.Insert(ctx, p, d, value, quality, reason, now); insertErr != nil {
					slog.Error("写入TDengine失败", "point_id", p.ID, "error", insertErr)
				}
			}
		}
	}
}
