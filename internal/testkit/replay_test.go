package testkit_test

import (
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/internal/testkit"
)

// Replay must surface a failing Apply instead of emitting a partial trace that
// could accidentally match.
func TestReplayFailsWhenApplyFails(t *testing.T) {
	// Two providers of the same service in one scope is a hard error.
	scenario := &testkit.Scenario{ID: "duplicate-provide", Steps: []testkit.Step{
		{Op: "register", As: "a", Name: "a", Provides: []string{"dup"}, Ready: boolPtr(true)},
		{Op: "register", As: "b", Name: "b", Provides: []string{"dup"}, Ready: boolPtr(true)},
	}}
	if _, err := testkit.Replay(scenario); err == nil {
		t.Fatal("a duplicate provider must fail the replay")
	}
}

// A plugin without a name is rejected by the runtime before it activates.
func TestReplayFailsForInvalidPlugin(t *testing.T) {
	scenario := &testkit.Scenario{ID: "unnamed", Steps: []testkit.Step{
		{Op: "register", As: "x", Name: ""},
	}}
	if _, err := testkit.Replay(scenario); err == nil {
		t.Fatal("an unnamed plugin must fail the replay")
	}
}

// Every dispatch mode must be exercised, including the bail and waterfall
// paths, because listener ordering and short-circuiting are the behaviour the
// event scenarios exist to pin down.
func TestReplayCoversEveryDispatchMode(t *testing.T) {
	scenario := &testkit.Scenario{ID: "dispatch-modes", Steps: []testkit.Step{
		{Op: "register", As: "h", Name: "h", Listeners: []testkit.Listener{
			{Event: "e", Seq: 1, Kind: "log"},
			{Event: "e", Seq: 2, Kind: "bail", Value: "stop"},
		}},
		{Op: "emit", Event: "e"},
		{Op: "serial", Event: "e"},
		{Op: "bail", Event: "e"},
		{Op: "waterfall", Event: "e"},
	}}
	trace, err := testkit.Replay(scenario)
	if err != nil {
		t.Fatal(err)
	}
	// The bail listener stops the chain, so the later listener never runs and
	// the waterfall only reaches the first listener before the veto.
	if strings.Count(trace, "listener h:1") < 3 {
		t.Fatalf("emit/serial/waterfall should each reach the first listener, got:\n%s", trace)
	}
	if !strings.Contains(trace, "listener h:2:bail=stop") {
		t.Fatalf("the bail value must appear in the trace, got:\n%s", trace)
	}
}

// DerefBool and the ready flag are part of the scenario contract: a conditional
// service whose ready flag is false must not activate its consumer.
func TestReadyFlagControlsAvailability(t *testing.T) {
	scenario := &testkit.Scenario{ID: "ready-flag", Steps: []testkit.Step{
		{Op: "register", As: "p", Name: "p", Provides: []string{"s"}, Ready: boolPtr(false)},
		{Op: "register", As: "c", Name: "c", Inject: []string{"s"}},
		{Op: "observe", Note: "retracted"},
		{Op: "setReady", As: "p", Ready: boolPtr(true)},
		{Op: "notify", Names: []string{"s"}},
		{Op: "observe", Note: "available"},
	}}
	trace, err := testkit.Replay(scenario)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(trace, "state c=pending") || !strings.Contains(trace, "state c=active") {
		t.Fatalf("the ready flag must gate the consumer, got:\n%s", trace)
	}
}

func boolPtr(value bool) *bool { return &value }
