package taskstate

import (
	"errors"
	"fmt"
	"strings"
)

// Errors this package returns.
var (
	// ErrInvalidEvent reports a task event that cannot be applied.
	ErrInvalidEvent = errors.New("taskstate: invalid event")
)

// Kind is the role of a recorded task event.
type Kind string

// The task event kinds this package names.
const (
	// KindGoalSet records that the goal was set or replaced.
	KindGoalSet Kind = "goal-set"
	// KindGoalDone records that the goal was marked done.
	KindGoalDone Kind = "goal-done"
	// KindPlanEnter records that plan mode was entered.
	KindPlanEnter Kind = "plan-enter"
	// KindPlanExit records that plan mode was exited.
	KindPlanExit Kind = "plan-exit"
	// KindTodoSet records one todo item's state.
	KindTodoSet Kind = "todo-set"
)

// Event is one recorded task fact.
type Event struct {
	// Kind is the event's role.
	Kind Kind
	// Text is the goal text, plan reason or todo text, depending on Kind.
	Text string
	// Done is the todo item's completion, for KindTodoSet.
	Done bool
}

// Validate reports whether a task event can be applied.
func (e Event) Validate() error {
	switch e.Kind {
	case KindGoalSet, KindTodoSet:
		if strings.TrimSpace(e.Text) == "" {
			return fmt.Errorf("%w: %s requires text", ErrInvalidEvent, e.Kind)
		}
	case KindGoalDone, KindPlanEnter, KindPlanExit:
		// A done or mode change carries no text.
	default:
		return fmt.Errorf("%w: unknown kind %q", ErrInvalidEvent, e.Kind)
	}
	return nil
}

// State is the projection of a task event log.
//
// It is a pure function of the events: applying the same events in the same order
// always produces the same State, which is what makes the current intention
// rebuildable rather than a mutable value someone forgot to update.
type State struct {
	// Goal is the current goal text, empty when none is set.
	Goal string
	// GoalDone reports whether the current goal is marked done.
	GoalDone bool
	// PlanMode reports whether plan mode is active.
	PlanMode bool
	// PlanReason is why plan mode was last entered.
	PlanReason string
	// Todos are the todo items in insertion order.
	Todos []Todo
}

// Todo is one todo item's projected state.
type Todo struct {
	// Text is the item's description.
	Text string
	// Done reports completion.
	Done bool
}

// Rebuild projects the task state from an event sequence.
//
// It is deliberately total: an invalid event returns an error rather than being
// skipped, because skipping would silently produce a state that disagrees with
// the log.
func Rebuild(events []Event) (State, error) {
	var state State
	index := map[string]int{}
	for position, event := range events {
		if err := event.Validate(); err != nil {
			return State{}, fmt.Errorf("event %d: %w", position, err)
		}
		switch event.Kind {
		case KindGoalSet:
			state.Goal = event.Text
			state.GoalDone = false
		case KindGoalDone:
			if state.Goal == "" {
				return State{}, fmt.Errorf("%w: goal-done with no goal", ErrInvalidEvent)
			}
			state.GoalDone = true
		case KindPlanEnter:
			state.PlanMode = true
			state.PlanReason = event.Text
		case KindPlanExit:
			state.PlanMode = false
		case KindTodoSet:
			if at, ok := index[event.Text]; ok {
				state.Todos[at].Done = event.Done
			} else {
				index[event.Text] = len(state.Todos)
				state.Todos = append(state.Todos, Todo{Text: event.Text, Done: event.Done})
			}
		}
	}
	return state, nil
}

// Append validates and appends one event, returning the new event slice.
//
// It does not mutate the input, so a caller that keeps the old slice still has
// the history before the append.
func Append(events []Event, event Event) ([]Event, error) {
	if err := event.Validate(); err != nil {
		return events, err
	}
	if event.Kind == KindGoalDone {
		// A goal must exist before it can be done; check against the current
		// projection so the invariant fails at append time, not at rebuild.
		if state, err := Rebuild(events); err == nil && state.Goal == "" {
			return events, fmt.Errorf("%w: goal-done with no goal", ErrInvalidEvent)
		}
	}
	out := make([]Event, len(events), len(events)+1)
	copy(out, events)
	return append(out, event), nil
}

// ShouldContinue reports whether a goal-driven run should continue.
//
// A goal that is set and not done means work remains; a done or absent goal means
// it does not. This is the one place the continuation rule lives, so a reader can
// point at it instead of inferring it from scattered conditionals.
func (s State) ShouldContinue() bool {
	return s.Goal != "" && !s.GoalDone
}

// OpenTodos returns the number of todo items not yet done.
func (s State) OpenTodos() int {
	open := 0
	for _, todo := range s.Todos {
		if !todo.Done {
			open++
		}
	}
	return open
}
