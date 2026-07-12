package platform

import (
	collectorengine "aquacontrolai/internal/engine/collector"
	writerengine "aquacontrolai/internal/engine/writer"
	"aquacontrolai/internal/model"
	"aquacontrolai/internal/protocol"
	pg "aquacontrolai/internal/repository/postgres"
	td "aquacontrolai/internal/repository/tdengine"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"math"
	"net"
	"strings"
	"time"
)

type Service struct {
	Store       *pg.Store
	Registry    *protocol.Registry
	Connections *collectorengine.Manager
	Writer      *writerengine.Engine
	Collector   *collectorengine.Engine
	TD          *td.Store
}

func (s *Service) ListDevices(ctx context.Context, keyword string) ([]model.Device, error) {
	items, e := s.Store.ListDevices(ctx, keyword)
	if e == nil && s.Connections != nil {
		for i := range items {
			if items[i].Enabled {
				items[i].ConnectionStatus = s.Connections.Status(items[i].ID)
			}
			items[i].LastOnlineAt, items[i].LastOfflineAt = s.Connections.Times(items[i].ID)
		}
	}
	return items, e
}
func (s *Service) SaveDevice(ctx context.Context, id *uuid.UUID, d *model.Device) error {
	d.Name = strings.TrimSpace(d.Name)
	if d.Name == "" || len([]rune(d.Name)) > 128 {
		return errors.New("设备名称长度必须为1~128")
	}
	if net.ParseIP(d.Host) == nil && !validHost(d.Host) {
		return errors.New("无效的IP地址或域名")
	}
	if d.Port < 1 || d.Port > 65535 || d.ConnectTimeout < 1 || d.ConnectTimeout > 60 || d.ReconnectInterval < 1 || d.ReconnectInterval > 3600 {
		return errors.New("连接参数超出范围")
	}
	factory, e := s.Registry.Get(d.ProtocolType)
	if e != nil {
		return e
	}
	var cfg map[string]any
	if e = json.Unmarshal(d.ProtocolConfig, &cfg); e != nil {
		return errors.New("protocol_config必须为对象")
	}
	if e = factory.ValidateConfig(cfg); e != nil {
		return e
	}
	if id == nil {
		return s.Store.CreateDevice(ctx, d)
	}
	d.ID = *id
	e = s.Store.UpdateDevice(ctx, d)
	if e == nil && s.Connections != nil {
		s.Connections.Invalidate(d.ID)
	}
	return e
}
func validHost(h string) bool {
	if len(h) < 1 || len(h) > 253 {
		return false
	}
	for _, p := range strings.Split(h, ".") {
		if len(p) < 1 || len(p) > 63 {
			return false
		}
		for _, r := range p {
			if !(r == '-' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
				return false
			}
		}
	}
	return true
}
func (s *Service) DeleteDevice(ctx context.Context, id uuid.UUID) error {
	e := s.Store.DeleteDevice(ctx, id)
	if e == nil && s.Connections != nil {
		s.Connections.Invalidate(id)
	}
	return e
}
func (s *Service) ListPoints(ctx context.Context, kind, keyword string) ([]pg.PointRow, error) {
	items, e := s.Store.ListPoints(ctx, kind, keyword, false)
	if e == nil && kind == "collection" && s.Collector != nil {
		for i := range items {
			items[i].LatestValue = s.Collector.Latest(items[i].ID)
		}
	}
	return items, e
}
func (s *Service) ListLogs(ctx context.Context) ([]map[string]any, error) {
	return s.Store.ListLogs(ctx)
}
func (s *Service) GetDevice(ctx context.Context, id uuid.UUID) (model.Device, error) {
	d, e := s.Store.GetDevice(ctx, id)
	if e == nil {
		if d.Enabled && s.Connections != nil {
			d.ConnectionStatus = s.Connections.Status(id)
			d.LastOnlineAt, d.LastOfflineAt = s.Connections.Times(id)
		} else {
			d.ConnectionStatus = "disabled"
		}
	}
	return d, e
}
func (s *Service) GetPoint(ctx context.Context, kind string, id uuid.UUID) (pg.PointRow, error) {
	p, e := s.Store.GetPoint(ctx, kind, id)
	if e == nil && kind == "collection" && s.Collector != nil {
		p.LatestValue = s.Collector.Latest(id)
	}
	return p, e
}
func (s *Service) Groups(ctx context.Context) ([]map[string]any, error) { return s.Store.Groups(ctx) }
func (s *Service) CreateGroup(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 64 {
		return errors.New("分组名称长度必须为1~64")
	}
	return s.Store.CreateGroup(ctx, name)
}
func (s *Service) UpdateGroup(ctx context.Context, oldName, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" || len([]rune(newName)) > 64 {
		return errors.New("分组名称长度必须为1~64")
	}
	return s.Store.UpdateGroup(ctx, oldName, newName)
}
func (s *Service) DeleteGroup(ctx context.Context, name string) error {
	return s.Store.DeleteGroup(ctx, name)
}
func (s *Service) FindDeviceByName(ctx context.Context, name string) (model.Device, error) {
	return s.Store.FindDeviceByName(ctx, name)
}
func (s *Service) FindPointByName(ctx context.Context, kind, name string) (pg.PointRow, error) {
	return s.Store.FindPointByName(ctx, kind, name)
}
func (s *Service) GetRetention(ctx context.Context) (int, error) { return s.Store.GetRetention(ctx) }
func (s *Service) SetRetention(ctx context.Context, days int) error {
	if days < 1 || days > 730 {
		return errors.New("history_retention_days必须为1~730")
	}
	if e := s.TD.SetRetention(ctx, days); e != nil {
		return fmt.Errorf("更新TDengine保留策略失败: %w", e)
	}
	return s.Store.SetRetention(ctx, days)
}
func (s *Service) SavePoint(ctx context.Context, kind string, p *pg.PointRow) error {
	p.Name = strings.TrimSpace(p.Name)
	p.GroupName = strings.TrimSpace(p.GroupName)
	if p.Name == "" || len([]rune(p.Name)) > 128 {
		return errors.New("点位名称长度必须为1~128")
	}
	if p.GroupName == "" {
		p.GroupName = "default"
	}
	d, e := s.Store.GetDevice(ctx, p.DeviceID)
	if e != nil {
		return errors.New("所属设备不存在")
	}
	f, e := s.Registry.Get(d.ProtocolType)
	if e != nil {
		return e
	}
	if e = f.ValidateAddress(p.Address, protocol.DataType(p.DataType), kind == "write"); e != nil {
		return e
	}
	if p.DataType != "BOOL" && p.DataType != "INT" && p.DataType != "REAL" {
		return errors.New("data_type无效")
	}
	if kind == "collection" {
		if p.CollectInterval < 1 {
			return errors.New("collect_interval必须至少1秒")
		}
		if p.HistoryInterval < 1 || p.HistoryInterval > 1440 {
			return errors.New("history_interval必须为1~1440")
		}
	}
	return s.Store.SavePoint(ctx, kind, p)
}
func (s *Service) DeletePoint(ctx context.Context, kind string, id uuid.UUID) error {
	return s.Store.DeletePoint(ctx, kind, id)
}
func (s *Service) ExecuteWrite(ctx context.Context, id uuid.UUID, value any, reason *string) (map[string]any, int, error) {
	p, e := s.Store.GetPoint(ctx, "write", id)
	if e != nil {
		return nil, 42004, e
	}
	numeric, e := typedNumber(p.DataType, value)
	if e != nil {
		return nil, 42001, e
	}
	if reason != nil && len([]rune(*reason)) > 500 {
		return nil, 42001, errors.New("reason最多500字符")
	}
	result, e := s.Writer.Execute(ctx, p, numeric)
	target := fmt.Sprintf("%v", value)
	status := "success"
	var readback, errorMessage *string
	if e != nil {
		status = "failed"
		m := "设备写入或回读失败"
		errorMessage = &m
	} else {
		r := fmt.Sprintf("%v", result.Readback)
		readback = &r
	}
	logID, logErr := s.Store.InsertWriteLog(ctx, p, target, readback, status, errorMessage, reason)
	if logErr != nil {
		return nil, 51001, logErr
	}
	data := map[string]any{"write_log_id": logID, "point_name": p.Name, "data_type": p.DataType, "value": value, "readback_value": nil, "result": status, "ts": time.Now()}
	if readback != nil {
		data["readback_value"] = result.Readback
	}
	if e != nil {
		data["error_message"] = *errorMessage
		return data, 51001, e
	}
	return data, 0, nil
}
func typedNumber(dt string, v any) (float64, error) {
	switch dt {
	case "BOOL":
		b, ok := v.(bool)
		if !ok {
			return 0, errors.New("BOOL只接受JSON boolean")
		}
		if b {
			return 1, nil
		}
		return 0, nil
	case "INT":
		n, ok := v.(float64)
		if !ok || math.Trunc(n) != n || n < -32768 || n > 32767 {
			return 0, errors.New("INT只接受16位整数")
		}
		return n, nil
	case "REAL":
		n, ok := v.(float64)
		if !ok {
			return 0, errors.New("REAL只接受JSON number")
		}
		return n, nil
	}
	return 0, errors.New("数据类型无效")
}
func IsNotFound(e error) bool { return errors.Is(e, pgx.ErrNoRows) }
