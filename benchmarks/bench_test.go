package benchmarks_test

import (
	"testing"

	"github.com/gosomea/deepseek-harness-go/capability/taskstate"
	"github.com/gosomea/deepseek-harness-go/plugins/clientview"
)

// BenchmarkTaskstateRebuild measures projecting a fixed event log to state.
func BenchmarkTaskstateRebuild(b *testing.B) {
	events := make([]taskstate.Event, 0, 100)
	for i := 0; i < 100; i++ {
		events = append(events, taskstate.Event{Kind: taskstate.KindTodoSet, Text: "item", Done: i%2 == 0})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := taskstate.Rebuild(events); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkClientDelta measures building an incremental delta from a fixed log.
func BenchmarkClientDelta(b *testing.B) {
	entries := make([]clientview.Entry, 0, 200)
	for i := 1; i <= 200; i++ {
		entries = append(entries, clientview.Entry{Seq: i, ID: "e", Text: "x"})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := clientview.BuildDelta(entries, 100); err != nil {
			b.Fatal(err)
		}
	}
}
