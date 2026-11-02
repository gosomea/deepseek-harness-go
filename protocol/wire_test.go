package protocol_test

import (
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/protocol"
)

func TestGuardWireFields(t *testing.T) {
	clean := map[string]string{"child": "child-1", "mode": "fork"}
	if err := protocol.GuardWireFields(clean); err != nil {
		t.Fatalf("clean rejected: %v", err)
	}
	leaks := []map[string]string{
		{"entry": "github.com/gosomea/deepseek-harness-go/capability/compaction.Entry"},
		{"owner": "*agent.Owner"},
		{"ref": "trpc.group/trpc-go/foo"},
		{"x": "session.Event"},
	}
	for i, leak := range leaks {
		if err := protocol.GuardWireFields(leak); !errors.Is(err, protocol.ErrInvalidEnvelope) {
			t.Fatalf("leak %d: err = %v", i, err)
		}
	}
}

func TestWireDTOValues(t *testing.T) {
	c := protocol.WireCompaction{Applied: true, Replaced: 3, SummaryID: "s1"}
	if !c.Applied || c.Replaced != 3 {
		t.Fatalf("compaction = %+v", c)
	}
	s := protocol.WireSubagent{Child: "c", Parent: "p", Mode: "fork", InheritThrough: 5, Depth: 1}
	if s.InheritThrough != 5 {
		t.Fatalf("subagent = %+v", s)
	}
	task := protocol.WireTask{ID: "j", Kind: "k", Status: "pending", Attempts: 1}
	if task.Status != "pending" {
		t.Fatalf("task = %+v", task)
	}
}
