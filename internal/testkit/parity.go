package testkit

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gosomea/deepseek-harness-go/cordis"
)

// Scenario is one shared behavioural case replayed by both runners.
//
// The JSON encoding is the interchange format: the TypeScript runner in
// testdata/parity/cordis reads the identical file. Field names stay stable so
// that a scenario recorded once keeps its meaning across implementations.
type Scenario struct {
	// ID uniquely identifies the scenario; it matches the file name stem.
	ID string `json:"id"`
	// Title states the single observable behaviour the scenario pins down.
	Title string `json:"title"`
	// Steps are executed in order. Each op is one observable action.
	Steps []Step `json:"steps"`
	// Divergences declares trace lines where the Go runtime intentionally
	// differs from the reference. Every entry must justify itself and must
	// actually occur.
	Divergences []Divergence `json:"divergences,omitempty"`
}

// Step is one operation in a scenario replay.
type Step struct {
	// Op selects the operation: register, settle, observe, setReady, notify,
	// dispose, close, emit, serial, bail, waterfall or update.
	Op string `json:"op"`
	// As names the node a step addresses.
	As string `json:"as,omitempty"`
	// Name is the diagnostic plugin name used at registration.
	Name string `json:"name,omitempty"`
	// Note labels an observation point in the emitted trace.
	Note string `json:"note,omitempty"`
	// Event is the event name for dispatch operations.
	Event string `json:"event,omitempty"`
	// Inject lists required services, which must be available before Apply runs.
	Inject []string `json:"inject,omitempty"`
	// Provides lists services the plugin publishes.
	Provides []string `json:"provides,omitempty"`
	// Ready is the initial availability reported by a conditional service.
	Ready *bool `json:"ready,omitempty"`
	// Config is the plugin configuration passed at registration or update.
	Config json.RawMessage `json:"config,omitempty"`
	// Validate installs an explicit configuration validator.
	Validate bool `json:"validate,omitempty"`
	// SelfDispose makes Apply dispose its own fiber while it runs.
	SelfDispose bool `json:"selfDispose,omitempty"`
	// Cleanups registers independent cleanups in the listed order.
	Cleanups []string `json:"cleanups,omitempty"`
	// Listeners registers event listeners.
	Listeners []Listener `json:"listeners,omitempty"`
	// Names lists services for a notify step.
	Names []string `json:"names,omitempty"`
}

// Divergence declares one intentional, reviewed difference between the Go
// trace and the reference trace for a scenario.
//
// This is not a normalization escape hatch. The comparator applies a
// declaration only where the Go trace emits exactly the declared Go block and
// the reference emits exactly the declared reference block, at the same point
// in the sequence; and every declaration must actually be used. A declaration
// therefore cannot paper over an unrelated mismatch, and one that stops
// describing reality fails the test. Both sides are blocks so that a change in
// message count (for example one reported error becoming two lines) stays
// visible instead of being smoothed away.
type Divergence struct {
	// Go lists the lines the Go runtime emits for this behaviour.
	Go []string `json:"go"`
	// Reference lists the lines the reference emits for the same behaviour.
	Reference []string `json:"reference"`
	// Reason explains why the behaviour differs and why it is acceptable.
	Reason string `json:"reason"`
}

// Listener describes one registered event listener.
type Listener struct {
	// Event is the event name the listener is registered for.
	Event string `json:"event"`
	// Seq is the diagnostic sequence number used in trace lines.
	Seq int `json:"seq"`
	// Kind is "log" for a plain listener or "bail" for a short-circuiting one.
	Kind string `json:"kind,omitempty"`
	// Value is the bail value returned by a bail listener.
	Value string `json:"value,omitempty"`
	// Prepend places the listener before existing ones.
	Prepend bool `json:"prepend,omitempty"`
}

// LoadScenario reads and validates a scenario file.
func LoadScenario(path string) (*Scenario, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var scenario Scenario
	if err := json.Unmarshal(body, &scenario); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if scenario.ID == "" || len(scenario.Steps) == 0 {
		return nil, fmt.Errorf("%s: scenario needs an id and steps", path)
	}
	return &scenario, nil
}

// ScenarioFiles returns the scenario files of a directory in name order.
func ScenarioFiles(dir string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	return matches, nil
}

// nodeState renders a fiber state the same way in both runners.
func nodeState(f *cordis.Fiber) string {
	if f == nil {
		return "absent"
	}
	return string(f.State())
}

// Replay executes a scenario against the Go runtime and returns the canonical
// trace. The trace format is deliberately narrow: it records observable events
// and explicit states, never fiber IDs, wall-clock values or registration
// order of independent nodes. Those vary between the two runtimes without
// describing behaviour, so normalizing them away keeps a mismatch meaningful.
func Replay(scenario *Scenario) (string, error) {
	var (
		mu    sync.Mutex
		lines []string
		nodes = map[string]*cordis.Fiber{}
		order []string
		ready = map[string]bool{}
		root  = cordis.New()
	)
	emit := func(line string) {
		mu.Lock()
		lines = append(lines, line)
		mu.Unlock()
	}
	note := func(format string, args ...any) { emit(fmt.Sprintf(format, args...)) }

	// The reference resolves lifecycle work through microtasks; the Go runtime
	// reconciles synchronously inside Plugin/Dispose. Waiting on the idle
	// signal reproduces the reference's "one observer turn" boundary without
	// depending on a sleep.
	settle := func() error {
		deadline, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return root.Wait(deadline)
	}

	var reportedCleanupError bool
	if err := runSteps(scenario, replayEnv{
		root: root, nodes: nodes, order: &order, ready: ready,
		reportedCleanupError: &reportedCleanupError,
		emit:                 emit, note: note, settle: settle,
	}); err != nil {
		return "", err
	}
	return strings.Join(lines, "\n") + "\n", nil
}

type replayEnv struct {
	root  *cordis.Context
	nodes map[string]*cordis.Fiber
	order *[]string
	ready map[string]bool
	// reportedCleanupError marks that a cleanup failure was already traced, so
	// repeated Wait calls do not duplicate the same report.
	reportedCleanupError *bool
	emit                 func(string)
	note                 func(string, ...any)
	settle               func() error
}

// register records a node name in first-registration order.
func (e replayEnv) register(name string) {
	for _, existing := range *e.order {
		if existing == name {
			return
		}
	}
	*e.order = append(*e.order, name)
}

// nodeOrder returns node names in registration order, which both runners
// preserve, so observed state lines stay comparable.
func (e replayEnv) nodeOrder() []string { return *e.order }

// closeDeadline bounds a Close call so a stuck cleanup fails the test rather
// than hanging it. The caller must invoke the returned cancel function.
func closeDeadline() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}
