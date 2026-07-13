package tdengine

import (
	"aquacontrolai/internal/model"
	pg "aquacontrolai/internal/repository/postgres"
	"context"
	"database/sql"
	"fmt"
	"github.com/google/uuid"
	_ "github.com/taosdata/driver-go/v3/taosRestful"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Store struct {
	DB       *sql.DB
	Database string
}

func (s *Store) Insert(ctx context.Context, p pg.PointRow, d model.Device, value *float64, quality int, reason *string, ts time.Time) error {
	valueLiteral := "NULL"
	if value != nil {
		valueLiteral = strconv.FormatFloat(*value, 'g', -1, 64)
	}
	reasonLiteral := "NULL"
	if reason != nil {
		reasonLiteral = sqlString(*reason)
	}
	q := fmt.Sprintf("INSERT INTO `%s`.`%s` USING `%s`.`collection_data` TAGS(%s,%s,%s) VALUES(%s,%s,%d,%s,%s,%s)", s.Database, TableName(p.ID), s.Database, sqlString(d.ID.String()), sqlString(d.Name), sqlString(p.DataType), sqlString(tdTimeLiteral(ts)), valueLiteral, quality, reasonLiteral, sqlString(p.ID.String()), sqlString(p.Name))
	_, e := s.DB.ExecContext(ctx, q)
	return e
}
func sqlString(v string) string { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }

var shanghai = time.FixedZone("Asia/Shanghai", 8*60*60)

func tdTimeLiteral(t time.Time) string {
	return t.In(shanghai).Format("2006-01-02T15:04:05.000-07:00")
}

func (s *Store) SetRetention(ctx context.Context, days int) error {
	if days < 1 || days > 730 {
		return fmt.Errorf("保留天数超出范围")
	}
	_, e := s.DB.ExecContext(ctx, fmt.Sprintf("ALTER DATABASE `%s` KEEP %d", s.Database, days))
	return e
}

type Sample struct {
	TS            time.Time `json:"ts"`
	Value         *float64  `json:"value"`
	Quality       string    `json:"quality"`
	QualityReason *string   `json:"quality_reason"`
}

func Open(ctx context.Context, dsn, database string) (*Store, error) {
	db, e := sql.Open("taosRestful", dsn)
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if e = db.PingContext(ctx); e != nil {
		db.Close()
		return nil, fmt.Errorf("TDengine 健康检查失败: %w", e)
	}
	return &Store{db, database}, nil
}

var tableRE = regexp.MustCompile(`^p_[0-9a-f]{32}$`)

func TableName(id uuid.UUID) string {
	n := "p_" + strings.ReplaceAll(strings.ToLower(id.String()), "-", "")
	if !tableRE.MatchString(n) {
		panic("invalid derived table")
	}
	return n
}
func (s *Store) HasData(ctx context.Context, id uuid.UUID) bool {
	q := fmt.Sprintf("SELECT COUNT(*) FROM `%s`.`%s`", s.Database, TableName(id))
	var n int
	return s.DB.QueryRowContext(ctx, q).Scan(&n) == nil && n > 0
}

// DropTables removes the per-point child tables for the supplied archived
// points. The caller is responsible for selecting only points that have no
// realtime metadata; TableName validates every derived identifier.
func (s *Store) DropTables(ctx context.Context, ids []uuid.UUID) (int, error) {
	removed := 0
	for _, id := range ids {
		if !s.HasData(ctx, id) {
			continue
		}
		q := fmt.Sprintf("DROP TABLE IF EXISTS `%s`.`%s`", s.Database, TableName(id))
		if _, e := s.DB.ExecContext(ctx, q); e != nil {
			return removed, e
		}
		removed++
	}
	return removed, nil
}
func (s *Store) Query(ctx context.Context, id uuid.UUID, start, end time.Time) ([]Sample, error) {
	q := fmt.Sprintf("SELECT ts,`value`,quality,quality_reason FROM `%s`.`%s` WHERE ts >= %s AND ts <= %s ORDER BY ts", s.Database, TableName(id), sqlString(tdTimeLiteral(start)), sqlString(tdTimeLiteral(end)))
	rows, e := s.DB.QueryContext(ctx, q)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := make([]Sample, 0)
	for rows.Next() {
		var x Sample
		var quality int
		if e = rows.Scan(&x.TS, &x.Value, &quality, &x.QualityReason); e != nil {
			return nil, e
		}
		x.TS = x.TS.In(shanghai)
		if quality == 0 {
			x.Quality = "good"
		} else {
			x.Quality = "bad"
		}
		out = append(out, x)
	}
	return out, rows.Err()
}
