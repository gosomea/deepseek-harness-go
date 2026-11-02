package clientview_test

import (
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/plugins/clientview"
)

func TestApplySnapshotAndDelta(t *testing.T) {
	all := []clientview.Entry{{Seq: 1, ID: "a"}, {Seq: 2, ID: "b"}, {Seq: 3, ID: "c"}}
	snap, _ := clientview.BuildSnapshot(all, 1)
	view := clientview.ApplySnapshot(snap)
	if view.ThroughSeq != 1 || len(view.Entries) != 1 {
		t.Fatalf("view = %+v", view)
	}
	delta, _ := clientview.BuildDelta(all, 1)
	next, err := view.ApplyDelta(delta)
	if err != nil {
		t.Fatalf("ApplyDelta: %v", err)
	}
	full, _ := clientview.BuildSnapshot(all, 3)
	if !clientview.SameView(next, clientview.ApplySnapshot(full)) {
		t.Fatalf("snapshot+delta != full snapshot: %+v vs full", next)
	}
	// A delta that does not start at throughSeq+1 is refused.
	if _, err := view.ApplyDelta(clientview.Delta{FromSeq: 5}); !errors.Is(err, clientview.ErrGap) {
		t.Fatalf("mismatched delta err = %v", err)
	}
}
