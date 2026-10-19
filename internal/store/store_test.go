package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHasCitation(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "single author year",
			body: "Notes remain readable years later (Luhmann, 1981).",
			want: true,
		},
		{
			name: "two authors with ampersand",
			body: "BM25 IDF clusters co-cited notes (Robertson & Zaragoza, 2009).",
			want: true,
		},
		{
			name: "year with suffix letter",
			body: "Observed in distributed consensus (Lamport, 1998a).",
			want: true,
		},
		{
			name: "no citation",
			body: "Unanchored scratch note without any reference.",
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasCitation(tc.body); got != tc.want {
				t.Fatalf("HasCitation(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}

func TestCleaveSyncAndErode(t *testing.T) {
	tmpDir := t.TempDir()
	st, err := Open(tmpDir)
	if err != nil {
		t.Fatalf("Open(%s): %v", tmpDir, err)
	}
	defer func() { _ = st.Close() }()

	monoPath := filepath.Join(tmpDir, "monoliths", "test-mono.md")
	if err := os.WriteFile(monoPath, []byte("# Test Monolith\n\nPermanent pattern."), 0o644); err != nil {
		t.Fatalf("write monolith: %v", err)
	}

	raw := "First stone body (Luhmann, 1981).\n---\nSecond cleaved stone body."
	ids, err := st.CleaveAndWrite(raw, "monolith:test-mono")
	if err != nil {
		t.Fatalf("CleaveAndWrite: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("expected 2 cleaved stones, got %d", len(ids))
	}

	count, err := st.SyncIndex(true)
	if err != nil {
		t.Fatalf("SyncIndex: %v", err)
	}
	if count != 3 {
		t.Fatalf("expected 3 indexed notes (2 stones + 1 monolith), got %d", count)
	}

	neighbors, err := st.GetTrailNeighbors([]string{ids[0]})
	if err != nil {
		t.Fatalf("GetTrailNeighbors: %v", err)
	}
	if len(neighbors) != 2 {
		t.Fatalf("expected 2 neighbors for %s (sequential + tether), got %v", ids[0], neighbors)
	}

	// Reject eroding monolith
	if err := st.Erode("monolith:test-mono"); err == nil {
		t.Fatalf("expected Erode to reject monolith ID")
	}

	// Erode second stone
	if err := st.Erode(ids[1]); err != nil {
		t.Fatalf("Erode(%s): %v", ids[1], err)
	}
	trashFile := filepath.Join(tmpDir, "quarry", ".trash", ids[1]+".md")
	if _, err := os.Stat(trashFile); err != nil {
		t.Fatalf("expected eroded stone in trash %s: %v", trashFile, err)
	}

	trailsRaw, err := os.ReadFile(filepath.Join(tmpDir, "trails.tsv"))
	if err != nil {
		t.Fatalf("read trails.tsv: %v", err)
	}
	if strings.Contains(string(trailsRaw), ids[1]) {
		t.Fatalf("expected eroded stone %s removed from trails.tsv", ids[1])
	}
}
