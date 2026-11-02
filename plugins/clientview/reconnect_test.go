package clientview_test

import (
	"testing"

	"github.com/gosomea/deepseek-harness-go/plugins/clientview"
)

func TestResumeIncremental(t *testing.T) {
	all := []clientview.Entry{{Seq: 1, ID: "a"}, {Seq: 2, ID: "b"}, {Seq: 3, ID: "c"}}
	hub, err := clientview.NewHub(all)
	if err != nil {
		t.Fatalf("NewHub: %v", err)
	}
	view, resynced, err := hub.Resume(1)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resynced {
		t.Fatal("should resume incrementally, not resync")
	}
	full := clientview.ApplySnapshot(hub.Latest())
	if !clientview.SameView(view, full) {
		t.Fatalf("resumed view != latest: %+v", view)
	}
}

func TestResumeFallbackToSnapshot(t *testing.T) {
	// Hub only holds entries from seq 5; a client at seq 1 fell behind.
	all := []clientview.Entry{{Seq: 5, ID: "x"}, {Seq: 6, ID: "y"}}
	hub, _ := clientview.NewHub(all)
	view, resynced, err := hub.Resume(1)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if !resynced {
		t.Fatal("expected resync fallback")
	}
	if view.ThroughSeq != 6 {
		t.Fatalf("view = %+v", view)
	}
}

func TestNewHubRejectsUnordered(t *testing.T) {
	if _, err := clientview.NewHub([]clientview.Entry{{Seq: 2}, {Seq: 1}}); err == nil {
		t.Fatal("unordered hub should error")
	}
}
