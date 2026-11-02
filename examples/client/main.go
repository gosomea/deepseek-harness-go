// Command client demonstrates the client projection: a snapshot plus a delta
// composes to the same view as a single full snapshot, and a client that fell
// behind resyncs from a snapshot instead of showing an incomplete view.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/gosomea/deepseek-harness-go/plugins/clientview"
)

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(out io.Writer) error {
	entries := []clientview.Entry{
		{Seq: 1, ID: "e1", Text: "hello"},
		{Seq: 2, ID: "e2", Text: "world"},
		{Seq: 3, ID: "e3", Text: "again"},
	}
	hub, err := clientview.NewHub(entries)
	if err != nil {
		return err
	}

	// A client that last saw seq 1 resumes incrementally.
	view, resynced, err := hub.Resume(1)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "resumed through: %d\n", view.ThroughSeq)
	fmt.Fprintf(out, "resynced: %t\n", resynced)

	// Snapshot + delta must equal a full snapshot.
	snap, err := clientview.BuildSnapshot(entries, 1)
	if err != nil {
		return err
	}
	delta, err := clientview.BuildDelta(entries, 1)
	if err != nil {
		return err
	}
	composed, err := clientview.ApplySnapshot(snap).ApplyDelta(delta)
	if err != nil {
		return err
	}
	full := clientview.ApplySnapshot(hub.Latest())
	fmt.Fprintf(out, "composed equals full: %t\n", clientview.SameView(composed, full))

	// A hub holding only seq 5+ forces a resync for a client at seq 1.
	late, err := clientview.NewHub([]clientview.Entry{{Seq: 5, ID: "e5", Text: "late"}})
	if err != nil {
		return err
	}
	lv, didResync, err := late.Resume(1)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "resync fallback: %t through %d\n", didResync, lv.ThroughSeq)
	return nil
}
