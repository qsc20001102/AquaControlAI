package tdengine

import (
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestTableName(t *testing.T) {
	id := uuid.MustParse("a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d")
	if got := TableName(id); got != "p_a1b2c3d4e5f64a7b8c9d0e1f2a3b4c5d" {
		t.Fatalf("unexpected table: %s", got)
	}
}
func TestSQLStringEscapesQuote(t *testing.T) {
	if got := sqlString("a'b"); got != "'a''b'" {
		t.Fatalf("unexpected literal: %s", got)
	}
}

func TestTDTimeLiteralUsesShanghaiOffset(t *testing.T) {
	ts := time.Date(2026, 7, 13, 2, 10, 10, 123000000, time.UTC)
	if got := tdTimeLiteral(ts); got != "2026-07-13T10:10:10.123+08:00" {
		t.Fatalf("unexpected TDengine timestamp literal: %s", got)
	}
}
