package writer

import (
	"aquacontrolai/internal/engine/collector"
	"aquacontrolai/internal/protocol"
	pg "aquacontrolai/internal/repository/postgres"
	"context"
	"errors"
	"math"
	"time"
)

type Engine struct {
	Manager *collector.Manager
	Store   *pg.Store
}
type Result struct {
	Value, Readback float64
	TS              time.Time
}

// REAL values are compared with a fixed protocol precision. The tolerance is
// intentionally not part of the write-point configuration anymore.
const realReadbackPrecision = 0.0001

func (e *Engine) Execute(ctx context.Context, p pg.PointRow, value float64) (Result, error) {
	if !p.WriteEnabled {
		return Result{}, errors.New("写入点未允许写入")
	}
	c, err := e.Manager.Connection(ctx, p.DeviceID)
	if err != nil {
		return Result{}, err
	}
	dt := protocol.DataType(p.DataType)
	for attempt := 0; attempt < 2; attempt++ {
		if err = c.Write(ctx, p.Address, dt, value); err != nil {
			continue
		}
		actual, readErr := c.Read(ctx, p.Address, dt)
		if readErr != nil {
			err = readErr
			continue
		}
		ok := actual == value
		if dt == protocol.Real {
			ok = math.Abs(actual-value) <= realReadbackPrecision
		}
		if ok {
			return Result{value, actual, time.Now()}, nil
		}
		err = errors.New("回读值与目标值不一致")
	}
	return Result{}, err
}
