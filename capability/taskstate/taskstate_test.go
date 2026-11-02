package taskstate_test

import (
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/capability/taskstate"
)

func TestRebuildOrder(t *testing.T) {
	events := []taskstate.Event{
		{Kind: taskstate.KindGoalSet, Text: "ship M9"},
		{Kind: taskstate.KindPlanEnter, Text: "explore"},
		{Kind: taskstate.KindTodoSet, Text: "a", Done: false},
		{Kind: taskstate.KindTodoSet, Text: "b", Done: false},
		{Kind: taskstate.KindTodoSet, Text: "a", Done: true},
		{Kind: taskstate.KindPlanExit},
	}
	state, err := taskstate.Rebuild(events)
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if state.Goal != "ship M9" || state.GoalDone {
		t.Fatalf("goal = %q done=%v", state.Goal, state.GoalDone)
	}
	if state.PlanMode {
		t.Fatal("plan mode should be off after exit")
	}
	if len(state.Todos) != 2 || !state.Todos[0].Done || state.Todos[1].Done {
		t.Fatalf("todos = %+v", state.Todos)
	}
	if state.OpenTodos() != 1 {
		t.Fatalf("open = %d", state.OpenTodos())
	}
	if !state.ShouldContinue() {
		t.Fatal("goal set and not done should continue")
	}
}

func TestRebuildInvalid(t *testing.T) {
	if _, err := taskstate.Rebuild([]taskstate.Event{{Kind: taskstate.KindGoalDone}}); !errors.Is(err, taskstate.ErrInvalidEvent) {
		t.Fatalf("goal-done without goal err = %v", err)
	}
	if _, err := taskstate.Rebuild([]taskstate.Event{{Kind: "bogus"}}); !errors.Is(err, taskstate.ErrInvalidEvent) {
		t.Fatalf("bogus kind err = %v", err)
	}
	if _, err := taskstate.Rebuild([]taskstate.Event{{Kind: taskstate.KindGoalSet}}); !errors.Is(err, taskstate.ErrInvalidEvent) {
		t.Fatalf("goal-set without text err = %v", err)
	}
}

func TestAppend(t *testing.T) {
	base := []taskstate.Event{{Kind: taskstate.KindGoalSet, Text: "g"}}
	next, err := taskstate.Append(base, taskstate.Event{Kind: taskstate.KindGoalDone})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if len(next) != 2 || len(base) != 1 {
		t.Fatalf("append mutated input: base=%d next=%d", len(base), len(next))
	}
	if _, err := taskstate.Append(nil, taskstate.Event{Kind: taskstate.KindGoalDone}); err == nil {
		t.Fatal("goal-done with no prior goal should fail")
	}
}
