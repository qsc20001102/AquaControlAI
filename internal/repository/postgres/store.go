package postgres

import (
	"aquacontrolai/internal/model"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type Store struct{ DB *pgxpool.Pool }

func Open(ctx context.Context, dsn string) (*Store, error) {
	db, e := pgxpool.New(ctx, dsn)
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if e = db.Ping(ctx); e != nil {
		db.Close()
		return nil, fmt.Errorf("PostgreSQL 健康检查失败: %w", e)
	}
	return &Store{db}, nil
}
func (s *Store) ListDevices(ctx context.Context, keyword string) ([]model.Device, error) {
	rows, e := s.DB.Query(ctx, `SELECT id,name,protocol_type,enabled,host,port,connect_timeout,reconnect_interval,protocol_config,created_at,updated_at FROM devices WHERE deleted=FALSE AND ($1='' OR name ILIKE '%'||$1||'%') ORDER BY name LIMIT 10000`, keyword)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []model.Device
	for rows.Next() {
		var d model.Device
		if e = rows.Scan(&d.ID, &d.Name, &d.ProtocolType, &d.Enabled, &d.Host, &d.Port, &d.ConnectTimeout, &d.ReconnectInterval, &d.ProtocolConfig, &d.CreatedAt, &d.UpdatedAt); e != nil {
			return nil, e
		}
		if d.Enabled {
			d.ConnectionStatus = "disconnected"
		} else {
			d.ConnectionStatus = "disabled"
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func (s *Store) CreateDevice(ctx context.Context, d *model.Device) error {
	return s.DB.QueryRow(ctx, `INSERT INTO devices(name,protocol_type,enabled,host,port,connect_timeout,reconnect_interval,protocol_config) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id,created_at,updated_at`, d.Name, d.ProtocolType, d.Enabled, d.Host, d.Port, d.ConnectTimeout, d.ReconnectInterval, d.ProtocolConfig).Scan(&d.ID, &d.CreatedAt, &d.UpdatedAt)
}
func (s *Store) UpdateDevice(ctx context.Context, d *model.Device) error {
	return s.DB.QueryRow(ctx, `UPDATE devices SET name=$2,protocol_type=$3,enabled=$4,host=$5,port=$6,connect_timeout=$7,reconnect_interval=$8,protocol_config=$9,updated_at=NOW() WHERE id=$1 AND deleted=FALSE RETURNING created_at,updated_at`, d.ID, d.Name, d.ProtocolType, d.Enabled, d.Host, d.Port, d.ConnectTimeout, d.ReconnectInterval, d.ProtocolConfig).Scan(&d.CreatedAt, &d.UpdatedAt)
}
func (s *Store) DeleteDevice(ctx context.Context, id uuid.UUID) error {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	tag, e := tx.Exec(ctx, `UPDATE devices SET deleted=TRUE,enabled=FALSE,updated_at=NOW() WHERE id=$1 AND deleted=FALSE`, id)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	for _, q := range []string{`UPDATE collection_points SET deleted=TRUE,enabled=FALSE,updated_at=NOW() WHERE device_id=$1 AND deleted=FALSE`, `UPDATE write_points SET deleted=TRUE,enabled=FALSE,write_enabled=FALSE,updated_at=NOW() WHERE device_id=$1 AND deleted=FALSE`} {
		if _, e = tx.Exec(ctx, q, id); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}

type PointRow struct {
	ID                uuid.UUID  `json:"id"`
	Name              string     `json:"name"`
	GroupName         string     `json:"group_name"`
	DeviceID          uuid.UUID  `json:"device_id"`
	DeviceName        string     `json:"device_name"`
	ProtocolType      string     `json:"protocol_type"`
	Address           string     `json:"address"`
	DataType          string     `json:"data_type"`
	Unit              *string    `json:"unit"`
	Enabled           bool       `json:"enabled"`
	CollectInterval   int        `json:"collect_interval,omitempty"`
	StoreHistory      bool       `json:"store_history,omitempty"`
	HistoryInterval   int        `json:"history_interval,omitempty"`
	HistoryStartedAt  *time.Time `json:"history_started_at,omitempty"`
	WriteEnabled      bool       `json:"write_enabled,omitempty"`
	ReadbackTolerance float64    `json:"readback_tolerance,omitempty"`
	LatestValue       any        `json:"latest_value"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

func (s *Store) GetPoint(ctx context.Context, kind string, id uuid.UUID) (PointRow, error) {
	items, e := s.ListPoints(ctx, kind, "", false)
	if e != nil {
		return PointRow{}, e
	}
	for _, p := range items {
		if p.ID == id {
			return p, nil
		}
	}
	return PointRow{}, pgx.ErrNoRows
}
func (s *Store) SavePoint(ctx context.Context, kind string, p *PointRow) error {
	if kind == "collection" {
		if p.ID == uuid.Nil {
			return s.DB.QueryRow(ctx, `INSERT INTO collection_points(name,group_name,device_id,enabled,address,data_type,unit,collect_interval,store_history,history_interval) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id,created_at,updated_at`, p.Name, p.GroupName, p.DeviceID, p.Enabled, p.Address, p.DataType, p.Unit, p.CollectInterval, p.StoreHistory, p.HistoryInterval).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
		}
		tag, e := s.DB.Exec(ctx, `UPDATE collection_points SET name=$2,group_name=$3,device_id=$4,enabled=$5,address=$6,data_type=$7,unit=$8,collect_interval=$9,store_history=$10,history_interval=$11,updated_at=NOW() WHERE id=$1 AND deleted=FALSE AND (history_started_at IS NULL OR (device_id=$4 AND data_type=$7))`, p.ID, p.Name, p.GroupName, p.DeviceID, p.Enabled, p.Address, p.DataType, p.Unit, p.CollectInterval, p.StoreHistory, p.HistoryInterval)
		if e == nil && tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return e
	}
	if p.ID == uuid.Nil {
		return s.DB.QueryRow(ctx, `INSERT INTO write_points(name,group_name,device_id,enabled,write_enabled,address,data_type,unit,readback_tolerance) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id,created_at,updated_at`, p.Name, p.GroupName, p.DeviceID, p.Enabled, p.WriteEnabled, p.Address, p.DataType, p.Unit, p.ReadbackTolerance).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
	}
	tag, e := s.DB.Exec(ctx, `UPDATE write_points SET name=$2,group_name=$3,device_id=$4,enabled=$5,write_enabled=$6,address=$7,data_type=$8,unit=$9,readback_tolerance=$10,updated_at=NOW() WHERE id=$1 AND deleted=FALSE`, p.ID, p.Name, p.GroupName, p.DeviceID, p.Enabled, p.WriteEnabled, p.Address, p.DataType, p.Unit, p.ReadbackTolerance)
	if e == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return e
}
func (s *Store) DeletePoint(ctx context.Context, kind string, id uuid.UUID) error {
	table := "collection_points"
	extra := ""
	if kind == "write" {
		table = "write_points"
		extra = ",write_enabled=FALSE"
	}
	q := fmt.Sprintf(`UPDATE %s SET deleted=TRUE,enabled=FALSE%s,updated_at=NOW() WHERE id=$1 AND deleted=FALSE`, table, extra)
	tag, e := s.DB.Exec(ctx, q, id)
	if e == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return e
}
func (s *Store) GetDevice(ctx context.Context, id uuid.UUID) (model.Device, error) {
	var d model.Device
	e := s.DB.QueryRow(ctx, `SELECT id,name,protocol_type,enabled,host,port,connect_timeout,reconnect_interval,protocol_config,created_at,updated_at FROM devices WHERE id=$1 AND deleted=FALSE`, id).Scan(&d.ID, &d.Name, &d.ProtocolType, &d.Enabled, &d.Host, &d.Port, &d.ConnectTimeout, &d.ReconnectInterval, &d.ProtocolConfig, &d.CreatedAt, &d.UpdatedAt)
	return d, e
}
func (s *Store) FindDeviceByName(ctx context.Context, name string) (model.Device, error) {
	var d model.Device
	e := s.DB.QueryRow(ctx, `SELECT id,name,protocol_type,enabled,host,port,connect_timeout,reconnect_interval,protocol_config,created_at,updated_at FROM devices WHERE name=$1 AND deleted=FALSE`, name).Scan(&d.ID, &d.Name, &d.ProtocolType, &d.Enabled, &d.Host, &d.Port, &d.ConnectTimeout, &d.ReconnectInterval, &d.ProtocolConfig, &d.CreatedAt, &d.UpdatedAt)
	return d, e
}
func (s *Store) FindPointByName(ctx context.Context, kind, name string) (PointRow, error) {
	items, e := s.ListPoints(ctx, kind, name, false)
	if e != nil {
		return PointRow{}, e
	}
	for _, p := range items {
		if p.Name == name {
			return p, nil
		}
	}
	return PointRow{}, pgx.ErrNoRows
}
func (s *Store) Groups(ctx context.Context) ([]map[string]any, error) {
	rows, e := s.DB.Query(ctx, `SELECT g.name,COUNT(p.id) FROM collection_groups g LEFT JOIN collection_points p ON p.group_name=g.name AND p.deleted=FALSE GROUP BY g.name ORDER BY g.name`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var name string
		var count int
		if e = rows.Scan(&name, &count); e != nil {
			return nil, e
		}
		out = append(out, map[string]any{"name": name, "count": count})
	}
	return out, rows.Err()
}
func (s *Store) CreateGroup(ctx context.Context, name string) error {
	_, e := s.DB.Exec(ctx, `INSERT INTO collection_groups(name) VALUES($1)`, name)
	return e
}
func (s *Store) UpdateGroup(ctx context.Context, oldName, newName string) error {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `INSERT INTO collection_groups(name) VALUES($1)`, newName); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE collection_points SET group_name=$2,updated_at=NOW() WHERE group_name=$1 AND deleted=FALSE`, oldName, newName); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `DELETE FROM collection_groups WHERE name=$1`, oldName); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) DeleteGroup(ctx context.Context, name string) error {
	if name == "default" {
		return fmt.Errorf("default分组不可删除")
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `UPDATE collection_points SET deleted=TRUE,enabled=FALSE,updated_at=NOW() WHERE group_name=$1 AND deleted=FALSE`, name); e != nil {
		return e
	}
	tag, e := tx.Exec(ctx, `DELETE FROM collection_groups WHERE name=$1`, name)
	if e == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) GetRetention(ctx context.Context) (int, error) {
	var days int
	e := s.DB.QueryRow(ctx, `SELECT (value #>> '{}')::integer FROM system_settings WHERE key='history_retention_days'`).Scan(&days)
	return days, e
}
func (s *Store) SetRetention(ctx context.Context, days int) error {
	_, e := s.DB.Exec(ctx, `INSERT INTO system_settings(key,value,updated_at) VALUES('history_retention_days',to_jsonb($1::integer),NOW()) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,updated_at=NOW()`, days)
	return e
}

func (s *Store) ListPoints(ctx context.Context, kind, keyword string, archived bool) ([]PointRow, error) {
	table := "collection_points"
	cols := `p.collect_interval,p.store_history,p.history_interval,p.history_started_at,FALSE,0`
	if kind == "write" {
		table = "write_points"
		cols = `NULL::double precision,NULL::double precision,0,FALSE,0,NULL::timestamptz,p.write_enabled,p.readback_tolerance`
	}
	deleted := "p.deleted=FALSE AND d.deleted=FALSE"
	if archived {
		deleted = "TRUE"
	}
	q := fmt.Sprintf(`SELECT p.id,p.name,p.group_name,p.device_id,d.name,d.protocol_type,p.address,p.data_type,p.unit,p.enabled,%s,p.created_at,p.updated_at FROM %s p JOIN devices d ON d.id=p.device_id WHERE %s AND ($1='' OR p.name ILIKE '%%'||$1||'%%' OR p.group_name ILIKE '%%'||$1||'%%') ORDER BY p.group_name,p.name LIMIT 1000`, cols, table, deleted)
	rows, e := s.DB.Query(ctx, q, keyword)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []PointRow
	for rows.Next() {
		var p PointRow
		if e = rows.Scan(&p.ID, &p.Name, &p.GroupName, &p.DeviceID, &p.DeviceName, &p.ProtocolType, &p.Address, &p.DataType, &p.Unit, &p.Enabled, &p.CollectInterval, &p.StoreHistory, &p.HistoryInterval, &p.HistoryStartedAt, &p.WriteEnabled, &p.ReadbackTolerance, &p.CreatedAt, &p.UpdatedAt); e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

type WriteLog struct {
	ID                            uuid.UUID `json:"id"`
	PointID                       uuid.UUID `json:"point_id"`
	PointName                     string    `json:"point_name"`
	DeviceID                      uuid.UUID `json:"device_id"`
	DeviceName, Address, DataType string
	Unit                          *string
	TargetValue                   any       `json:"target_value"`
	ReadbackValue                 any       `json:"readback_value"`
	Result                        string    `json:"result"`
	ErrorMessage                  *string   `json:"error_message"`
	Reason                        *string   `json:"reason"`
	CreatedAt                     time.Time `json:"created_at"`
}

func (s *Store) ListLogs(ctx context.Context) ([]map[string]any, error) {
	rows, e := s.DB.Query(ctx, `SELECT id,point_id,point_name,device_id,device_name,address,data_type,unit,target_value,readback_value,result,error_message,reason,created_at FROM write_logs ORDER BY created_at DESC LIMIT 100`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, pid, did uuid.UUID
		var pn, dn, a, dt, tv, res string
		var unit, rv, em, reason *string
		var ts time.Time
		if e = rows.Scan(&id, &pid, &pn, &did, &dn, &a, &dt, &unit, &tv, &rv, &res, &em, &reason, &ts); e != nil {
			return nil, e
		}
		out = append(out, map[string]any{"id": id, "point_id": pid, "point_name": pn, "device_id": did, "device_name": dn, "address": a, "data_type": dt, "unit": unit, "target_value": parseValue(dt, tv), "readback_value": parseOptional(dt, rv), "result": res, "error_message": em, "reason": reason, "source": "manual", "operator": nil, "created_at": ts})
	}
	return out, rows.Err()
}
func parseValue(dt, v string) any {
	var out any
	if dt == "BOOL" {
		json.Unmarshal([]byte(v), &out)
		return out
	}
	json.Unmarshal([]byte(v), &out)
	return out
}
func parseOptional(dt string, v *string) any {
	if v == nil {
		return nil
	}
	return parseValue(dt, *v)
}
func (s *Store) InsertWriteLog(ctx context.Context, p PointRow, target string, readback *string, result string, errorMessage *string, reason *string) (uuid.UUID, error) {
	var id uuid.UUID
	e := s.DB.QueryRow(ctx, `INSERT INTO write_logs(point_id,point_name,device_id,device_name,address,data_type,unit,source,target_value,readback_value,result,error_message,operator,reason) VALUES($1,$2,$3,$4,$5,$6,$7,'manual',$8,$9,$10,$11,NULL,$12) RETURNING id`, p.ID, p.Name, p.DeviceID, p.DeviceName, p.Address, p.DataType, p.Unit, target, readback, result, errorMessage, reason).Scan(&id)
	return id, e
}
func IsConflict(err error) bool {
	return err != nil && (errors.Is(err, pgx.ErrNoRows) || contains(err.Error(), "duplicate key"))
}
func (s *Store) MarkHistoryStarted(ctx context.Context, id uuid.UUID) error {
	_, e := s.DB.Exec(ctx, `UPDATE collection_points SET history_started_at=COALESCE(history_started_at,NOW()) WHERE id=$1`, id)
	return e
}
func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
