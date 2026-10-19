package engine

import (
	"math"
	"testing"
	"time"

	"cairn/internal/store"
)

func TestApplyWeathering(t *testing.T) {
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	sixMonthsAgo := now.Add(-time.Duration(6.0 * 24.0 * 30.4375 * float64(time.Hour))).Unix()

	tests := []struct {
		name     string
		note     store.Note
		rawScore float64
		want     float64
	}{
		{
			name:     "carved monolith never decays and gets 1.20x boost",
			note:     store.Note{ID: "monolith:test", IsMonolith: true, CreatedAt: sixMonthsAgo},
			rawScore: 1.0,
			want:     1.20,
		},
		{
			name:     "anchored stone decays with lambda=0.03",
			note:     store.Note{ID: "01K7P1A0M1R8Z2X4V6N8B0C2D4", HasCitation: true, CreatedAt: sixMonthsAgo},
			rawScore: 1.0,
			want:     math.Exp(-0.03 * 6.0),
		},
		{
			name:     "unanchored stone decays with lambda=0.20",
			note:     store.Note{ID: "01K7P1H4M6N8B0V2C4X6Z8A0S2", CreatedAt: sixMonthsAgo},
			rawScore: 1.0,
			want:     math.Exp(-0.20 * 6.0),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ApplyWeathering(tc.rawScore, tc.note, now)
			if math.Abs(got-tc.want) > 1e-4 {
				t.Fatalf("ApplyWeathering() = %.6f, want %.6f", got, tc.want)
			}
		})
	}
}

func TestComputeBridgeScore(t *testing.T) {
	tokA := TokenSet("sqlite fts5 lexical bm25 ranking query", 3)
	tokB := TokenSet("bubbletea terminal editor execprocess native", 3)
	cand := TokenSet("sqlite fts5 query results rendered inside bubbletea terminal pane", 3)

	score, ok := ComputeBridgeScore(cand, tokA, tokB)
	if !ok || score <= 0 {
		t.Fatalf("expected valid positive BridgeScore for stepping stone, got score=%v ok=%v", score, ok)
	}
}
