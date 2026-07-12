package tdengine

import (
	"github.com/google/uuid"
	"testing"
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
