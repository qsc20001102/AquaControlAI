package platform

import (
	td "aquacontrolai/internal/repository/tdengine"
	"testing"
	"time"
)

func sampleAt(ts time.Time, value float64) td.Sample {
	return td.Sample{TS: ts, Value: &value, Quality: "good"}
}

func TestWithHistoryGapsInsertsNullBreakpointForLongOutage(t *testing.T) {
	start := time.Date(2026, 7, 13, 1, 59, 0, 0, time.UTC)
	data := []td.Sample{
		sampleAt(start, 1),
		sampleAt(start.Add(time.Minute), 2),
		sampleAt(start.Add(6*time.Hour), 3),
	}

	got := withHistoryGaps(data, time.Minute)

	if len(got) != 4 {
		t.Fatalf("expected gap breakpoint, got %d rows", len(got))
	}
	if got[2].Value != nil || got[2].Quality != "none" {
		t.Fatalf("expected null gap marker, got value=%v quality=%q", got[2].Value, got[2].Quality)
	}
	if !got[2].TS.After(got[1].TS) || !got[2].TS.Before(got[3].TS) {
		t.Fatalf("gap marker should be inside outage: marker=%v before=%v after=%v", got[2].TS, got[1].TS, got[3].TS)
	}
}

func TestWithHistoryGapsAllowsExpectedIntervalJitter(t *testing.T) {
	start := time.Date(2026, 7, 13, 1, 59, 0, 0, time.UTC)
	data := []td.Sample{
		sampleAt(start, 1),
		sampleAt(start.Add(2*time.Minute), 2),
		sampleAt(start.Add(3*time.Minute), 3),
	}

	got := withHistoryGaps(data, time.Minute)

	if len(got) != len(data) {
		t.Fatalf("expected no gap breakpoint for short jitter, got %d rows", len(got))
	}
}
