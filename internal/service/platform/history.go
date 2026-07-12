package platform

import (
	collector "aquacontrolai/internal/engine/collector"
	pg "aquacontrolai/internal/repository/postgres"
	td "aquacontrolai/internal/repository/tdengine"
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/google/uuid"
	"math"
	"sort"
	"time"
)

type History struct {
	PG        *pg.Store
	TD        *td.Store
	Collector interface {
		Latest(uuid.UUID) *collector.LatestValue
	}
}
type Series struct {
	PointID     uuid.UUID   `json:"point_id"`
	PointName   string      `json:"point_name"`
	DataType    string      `json:"data_type"`
	Unit        *string     `json:"unit"`
	Sampled     bool        `json:"sampled"`
	RawCount    int         `json:"raw_count"`
	SampleCount int         `json:"sample_count"`
	Data        []td.Sample `json:"data"`
}

func (h *History) Tree(ctx context.Context) ([]map[string]any, error) {
	points, e := h.PG.ListPoints(ctx, "collection", "", true)
	if e != nil {
		return nil, e
	}
	groups := map[string][]map[string]any{}
	for _, p := range points {
		active := p.Enabled && p.StoreHistory
		has := h.TD.HasData(ctx, p.ID)
		if !active && !has {
			continue
		}
		life := "active"
		if !active {
			life = "archived"
		}
		var latest any
		if h.Collector != nil && life == "active" {
			latest = h.Collector.Latest(p.ID)
		}
		groups[p.GroupName] = append(groups[p.GroupName], map[string]any{"id": p.ID, "name": p.Name, "type": "collection", "data_type": p.DataType, "unit": p.Unit, "history_interval": p.HistoryInterval, "device_id": p.DeviceID, "device_name": p.DeviceName, "group_name": p.GroupName, "lifecycle_status": life, "has_history_data": has, "latest_value": latest})
	}
	names := make([]string, 0, len(groups))
	for n := range groups {
		names = append(names, n)
	}
	sort.Strings(names)
	tree := []map[string]any{}
	for _, n := range names {
		hash := sha256.Sum256([]byte(n))
		tree = append(tree, map[string]any{"id": fmt.Sprintf("group_%x", hash[:8]), "name": n, "type": "group", "children": groups[n]})
	}
	tree = append(tree, map[string]any{"id": "internal-data", "name": "内部数据", "type": "reserved", "children": []map[string]any{{"id": "placeholder", "name": "暂无数据", "type": "placeholder", "disabled": true}}})
	return tree, nil
}
func (h *History) Query(ctx context.Context, ids []uuid.UUID, start, end time.Time, max int) ([]Series, error) {
	meta, e := h.PG.ListPoints(ctx, "collection", "", true)
	if e != nil {
		return nil, e
	}
	byID := map[uuid.UUID]pg.PointRow{}
	for _, p := range meta {
		byID[p.ID] = p
	}
	out := make([]Series, 0, len(ids))
	for _, id := range ids {
		p, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("点位元数据不存在: %s", id)
		}
		data, e := h.TD.Query(ctx, id, start, end)
		if e != nil {
			return nil, e
		}
		raw := len(data)
		if raw > max {
			data = minMax(data, max)
		}
		out = append(out, Series{id, p.Name, p.DataType, p.Unit, raw > len(data), raw, len(data), data})
	}
	return out, nil
}
func minMax(data []td.Sample, max int) []td.Sample {
	if len(data) <= max {
		return data
	}
	keep := map[int]bool{0: true, len(data) - 1: true}
	bucket := float64(len(data)) / float64(max/2)
	for b := 0; b < max/2; b++ {
		lo, hi := int(float64(b)*bucket), int(float64(b+1)*bucket)
		if hi > len(data) {
			hi = len(data)
		}
		minI, maxI := -1, -1
		for i := lo; i < hi; i++ {
			if data[i].Value == nil {
				keep[i] = true
				continue
			}
			if minI < 0 || *data[i].Value < *data[minI].Value {
				minI = i
			}
			if maxI < 0 || *data[i].Value > *data[maxI].Value {
				maxI = i
			}
			if i > 0 && data[i].Quality != data[i-1].Quality {
				keep[i-1] = true
				keep[i] = true
			}
		}
		if minI >= 0 {
			keep[minI] = true
			keep[maxI] = true
		}
	}
	idx := make([]int, 0, len(keep))
	for i := range keep {
		idx = append(idx, i)
	}
	sort.Ints(idx)
	if len(idx) > max {
		idx = idx[:max]
	}
	out := make([]td.Sample, 0, len(idx))
	for _, i := range idx {
		out = append(out, data[i])
	}
	return out
}

type TableValue struct {
	Value         *float64   `json:"value"`
	Quality       string     `json:"quality"`
	QualityReason *string    `json:"quality_reason"`
	MatchedTS     *time.Time `json:"matched_ts"`
}
type TableColumn struct {
	PointID   uuid.UUID    `json:"point_id"`
	PointName string       `json:"point_name"`
	Unit      *string      `json:"unit"`
	Data      []TableValue `json:"data"`
}
type TableResult struct {
	TimeColumn []time.Time   `json:"time_column"`
	Columns    []TableColumn `json:"columns"`
}

func (h *History) QueryTable(ctx context.Context, ids []uuid.UUID, start, end time.Time, minutes int) (TableResult, error) {
	step := time.Duration(minutes) * time.Minute
	times := []time.Time{}
	for t := start; !t.After(end); t = t.Add(step) {
		times = append(times, t)
	}
	series, e := h.Query(ctx, ids, start.Add(-step/2), end.Add(step/2), 10000)
	if e != nil {
		return TableResult{}, e
	}
	res := TableResult{TimeColumn: times, Columns: []TableColumn{}}
	for _, s := range series {
		col := TableColumn{s.PointID, s.PointName, s.Unit, make([]TableValue, 0, len(times))}
		for _, target := range times {
			best := -1
			bestDist := time.Duration(math.MaxInt64)
			for i, x := range s.Data {
				d := x.TS.Sub(target)
				if d < 0 {
					d = -d
				}
				if d <= step/2 && (d < bestDist || (d == bestDist && best >= 0 && x.TS.Before(s.Data[best].TS))) {
					best, bestDist = i, d
				}
			}
			if best < 0 {
				col.Data = append(col.Data, TableValue{nil, "none", nil, nil})
			} else {
				x := s.Data[best]
				ts := x.TS
				col.Data = append(col.Data, TableValue{x.Value, x.Quality, x.QualityReason, &ts})
			}
		}
		res.Columns = append(res.Columns, col)
	}
	return res, nil
}
