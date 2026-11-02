package clientview_test

import (
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/plugins/clientview"
)

func entries() []clientview.Entry {
	return []clientview.Entry{
		{Seq: 1, ID: "e1", Text: "a"},
		{Seq: 2, ID: "e2", Text: "b"},
		{Seq: 3, ID: "e3", Text: "c"},
	}
}

func TestBuildSnapshot(t *testing.T) {
	s, err := clientview.BuildSnapshot(entries(), 2)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if s.ThroughSeq != 2 || len(s.Entries) != 2 {
		t.Fatalf("snapshot = %+v", s)
	}
	// Unordered input is an error.
	if _, err := clientview.BuildSnapshot([]clientview.Entry{{Seq: 2}, {Seq: 1}}, 2); err == nil {
		t.Fatal("unordered snapshot should error")
	}
}

func TestBuildDelta(t *testing.T) {
	d, err := clientview.BuildDelta(entries(), 1)
	if err != nil {
		t.Fatalf("BuildDelta: %v", err)
	}
	if d.FromSeq != 2 || len(d.Entries) != 2 {
		t.Fatalf("delta = %+v", d)
	}
	// Gap: earliest entry is 5 but client asks after 1.
	gap := []clientview.Entry{{Seq: 5, ID: "x"}}
	if _, err := clientview.BuildDelta(gap, 1); !errors.Is(err, clientview.ErrGap) {
		t.Fatalf("gap err = %v", err)
	}
}
