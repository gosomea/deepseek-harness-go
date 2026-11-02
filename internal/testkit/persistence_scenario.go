package testkit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/persistence"
	"github.com/gosomea/deepseek-harness-go/plugins/sessionjsonl"
	"github.com/gosomea/deepseek-harness-go/session"
)

// ErrInvalidPersistenceScenario reports a persistence fixture that cannot be
// replayed.
var ErrInvalidPersistenceScenario = errors.New("testkit: invalid persistence scenario")

// PersistenceScenario is one durable-session behaviour described as data.
//
// The damage is declarative rather than embedded as bytes, so a reader can see
// what was broken without decoding base64, and the runner owns how it is applied.
type PersistenceScenario struct {
	// ID is the scenario's stable name.
	ID string `json:"id"`
	// Title explains what the scenario demonstrates.
	Title string `json:"title"`
	// Events are the stored events, in order.
	Events []ScenarioEvent `json:"events"`
	// Damage describes the corruption to apply to a copy, when any.
	Damage *ScenarioDamage `json:"damage,omitempty"`
	// Expect lists the observable outcomes.
	Expect ScenarioExpectation `json:"expect"`
}

// ScenarioEvent is one stored event.
type ScenarioEvent struct {
	// Type is the event type.
	Type string `json:"type"`
	// Data is a flat payload.
	Data map[string]any `json:"data,omitempty"`
	// Message is a message payload; it is nested for the four nested types.
	Message *ScenarioMessage `json:"message,omitempty"`
	// Surface is "append", "replace", or empty for a log-only event.
	Surface string `json:"surface,omitempty"`
	// Shadows lists the sequence numbers a "replace" surface supersedes.
	//
	// The log refuses a replacement that does not cite every node it shadows, so
	// a fixture must be able to express that: without it, a replace scenario
	// would be impossible to write rather than merely uncommon.
	Shadows []int `json:"shadows,omitempty"`
}

// ScenarioMessage is a message payload in a fixture.
type ScenarioMessage struct {
	// ID is the message identity.
	ID string `json:"id"`
	// Role is the message role.
	Role string `json:"role"`
	// Text is the message text.
	Text string `json:"text"`
}

// ScenarioDamage is one declarative corruption.
type ScenarioDamage struct {
	// Kind is "torn-tail", "middle-checksum", or "version".
	Kind string `json:"kind"`
	// Bytes are appended for a torn tail.
	Bytes string `json:"bytes,omitempty"`
	// Version replaces the header version.
	Version int `json:"version,omitempty"`
}

// ScenarioExpectation lists what the trace must show.
type ScenarioExpectation struct {
	// Complete is the expected count of complete records.
	Complete int `json:"complete,omitempty"`
	// Repairable is the expected repairability.
	Repairable *bool `json:"repairable,omitempty"`
	// Reason is expected to be empty when it is set to "".
	Reason *string `json:"reason,omitempty"`
	// CompleteAfterRepair is the expected count after a repair.
	CompleteAfterRepair int `json:"completeAfterRepair,omitempty"`
	// PrefixUnchanged asserts the repair left the prefix byte-identical.
	PrefixUnchanged bool `json:"prefixUnchanged,omitempty"`
	// OpenTurn is the expected resumed-turn state.
	OpenTurn *bool `json:"openTurn,omitempty"`
	// Unsettled lists the expected unsettled call ids.
	Unsettled []string `json:"unsettled,omitempty"`
	// PendingInputs is the expected unclaimed-input count.
	PendingInputs *int `json:"pendingInputs,omitempty"`
	// NeedsAttention is the expected attention flag.
	NeedsAttention *bool `json:"needsAttention,omitempty"`
	// HistoryRoles lists the expected derived roles.
	HistoryRoles []string `json:"historyRoles,omitempty"`
	// SecondWriterRefused asserts a second write open was refused.
	SecondWriterRefused bool `json:"secondWriterRefused,omitempty"`
	// ReadAllowedWhileWriting asserts a reader worked while a writer held it.
	ReadAllowedWhileWriting bool `json:"readAllowedWhileWriting,omitempty"`
	// ReleasedAfterClose asserts the writer claim was released.
	ReleasedAfterClose bool `json:"releasedAfterClose,omitempty"`
	// OpenRefused asserts the open was refused.
	OpenRefused bool `json:"openRefused,omitempty"`
	// ErrorKind is the expected error category.
	ErrorKind string `json:"errorKind,omitempty"`
	// FileUnchanged asserts a refused operation left the artifact alone.
	FileUnchanged bool `json:"fileUnchanged,omitempty"`
}

// LoadPersistenceScenario reads and validates one fixture.
func LoadPersistenceScenario(path string) (PersistenceScenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PersistenceScenario{}, err
	}
	var scenario PersistenceScenario
	if err := json.Unmarshal(data, &scenario); err != nil {
		return PersistenceScenario{}, fmt.Errorf("%w: %s: %v", ErrInvalidPersistenceScenario, path, err)
	}
	if err := scenario.Validate(); err != nil {
		return PersistenceScenario{}, fmt.Errorf("%w: %s: %v", ErrInvalidPersistenceScenario, path, err)
	}
	return scenario, nil
}

// Validate reports whether the scenario can be replayed.
func (s PersistenceScenario) Validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return fmt.Errorf("%w: scenario requires an id", ErrInvalidPersistenceScenario)
	}
	if len(s.Events) == 0 {
		return fmt.Errorf("%w: scenario %q requires at least one event", ErrInvalidPersistenceScenario, s.ID)
	}
	for index, event := range s.Events {
		if strings.TrimSpace(event.Type) == "" {
			return fmt.Errorf("%w: scenario %q event %d has no type", ErrInvalidPersistenceScenario, s.ID, index)
		}
		if event.Message != nil && strings.TrimSpace(event.Message.Role) == "" {
			return fmt.Errorf("%w: scenario %q event %d message has no role", ErrInvalidPersistenceScenario, s.ID, index)
		}
		switch event.Surface {
		case "", "append", "replace":
		default:
			return fmt.Errorf("%w: scenario %q event %d has unknown surface %q", ErrInvalidPersistenceScenario, s.ID, index, event.Surface)
		}
	}
	if s.Damage != nil {
		switch s.Damage.Kind {
		case "torn-tail", "middle-checksum", "version":
		default:
			return fmt.Errorf("%w: scenario %q has unknown damage kind %q", ErrInvalidPersistenceScenario, s.ID, s.Damage.Kind)
		}
	}
	return nil
}

// PersistenceScenarioDir returns the fixture directory for a repository root.
func PersistenceScenarioDir(root string) string {
	return filepath.Join(root, "testdata", "parity", "persistence", "scenarios")
}

// ReplayPersistenceScenario runs one scenario and returns its trace.
//
// The trace is line-oriented so two runs can be compared as text. The whole
// scenario runs under one temporary directory that is removed afterwards, which
// is what keeps the replay away from any real session data.
func ReplayPersistenceScenario(scenario PersistenceScenario) (HarnessTrace, error) {
	if err := scenario.Validate(); err != nil {
		return HarnessTrace{}, err
	}
	ctx := context.Background()
	root, err := os.MkdirTemp("", "persistence-scenario-")
	if err != nil {
		return HarnessTrace{}, err
	}
	defer func() { _ = os.RemoveAll(root) }()

	provider, err := sessionjsonl.New(sessionjsonl.Options{DataDir: root})
	if err != nil {
		return HarnessTrace{}, err
	}
	id := persistence.SessionID("scenario")
	if _, err := provider.Create(ctx, persistence.CreateOptions{ID: id}); err != nil {
		return HarnessTrace{}, err
	}
	handle, err := provider.Open(ctx, persistence.OpenOptions{ID: id, Access: persistence.AccessWrite})
	if err != nil {
		return HarnessTrace{}, err
	}
	at := time.UnixMilli(1_700_000_000_000)
	for index, event := range scenario.Events {
		input, err := event.appendInput()
		if err != nil {
			return HarnessTrace{}, err
		}
		// A fixed timestamp keeps two runs comparable.
		input.Time = &at
		if _, err := handle.Append(ctx, input); err != nil {
			return HarnessTrace{}, fmt.Errorf("event %d (%s): %w", index, event.Type, err)
		}
	}

	trace := HarnessTrace{}
	// The single-writer expectations are checked while the writer is still open.
	if scenario.Expect.SecondWriterRefused {
		_, err := provider.Open(ctx, persistence.OpenOptions{ID: id, Access: persistence.AccessWrite})
		trace.Lines = append(trace.Lines,
			fmt.Sprintf("secondWriterRefused=%v", errors.Is(err, persistence.ErrWriterExists)))
	}
	if scenario.Expect.ReadAllowedWhileWriting {
		reader, err := provider.Open(ctx, persistence.OpenOptions{ID: id, Access: persistence.AccessRead})
		allowed := err == nil
		if allowed {
			_, err = reader.Read(ctx, 0)
			allowed = err == nil
			_ = reader.Close()
		}
		trace.Lines = append(trace.Lines, fmt.Sprintf("readAllowedWhileWriting=%v", allowed))
	}
	if err := handle.Flush(ctx); err != nil {
		return HarnessTrace{}, err
	}
	if err := handle.Close(); err != nil {
		return HarnessTrace{}, err
	}
	if scenario.Expect.ReleasedAfterClose {
		_, err := provider.Open(ctx, persistence.OpenOptions{ID: id, Access: persistence.AccessWrite})
		released := err == nil
		trace.Lines = append(trace.Lines, fmt.Sprintf("releasedAfterClose=%v", released))
		_ = err
	}

	return replayDurability(ctx, trace, provider, root, id, scenario)
}

// appendInput converts a fixture event into an append input.
func (e ScenarioEvent) appendInput() (session.AppendInput, error) {
	input := session.AppendInput{Type: e.Type}
	if len(e.Data) > 0 {
		encoded, err := json.Marshal(e.Data)
		if err != nil {
			return session.AppendInput{}, err
		}
		input.Data = encoded
	}
	if e.Message != nil {
		message, err := buildScenarioMessage(*e.Message)
		if err != nil {
			return session.AppendInput{}, err
		}
		input.Data = message
		// The four nested message types wrap the message under "message"; a user
		// message is the payload itself, which is a difference the log's surface
		// rules enforce.
		if e.Type != "user/message" {
			nested, err := persistence.NestMessage(message)
			if err != nil {
				return session.AppendInput{}, err
			}
			input.Data = nested
		}
	}
	if input.Data == nil {
		input.Data = []byte(`{}`)
	}
	switch e.Surface {
	case "append":
		input.SurfaceOp = &session.SurfaceOp{Op: session.SurfaceAppend}
	case "replace":
		input.SurfaceOp = &session.SurfaceOp{Op: session.SurfaceReplace}
		seqs := make([]session.Seq, 0, len(e.Shadows))
		for _, seq := range e.Shadows {
			seqs = append(seqs, session.Seq(seq))
		}
		input.SourceEventSeqs = seqs
	}
	return input, nil
}

// buildScenarioMessage renders a fixture message.
func buildScenarioMessage(message ScenarioMessage) ([]byte, error) {
	switch message.Role {
	case "user":
		built, err := llm.NewUserMessage(message.Text)
		if err != nil {
			return nil, err
		}
		built.ID = llm.MessageID(message.ID)
		return llm.MarshalMessage(built)
	case "assistant":
		built, err := llm.NewAssistantMessage("fake", "fake-1", llm.TextContent(message.Text))
		if err != nil {
			return nil, err
		}
		built.ID = llm.MessageID(message.ID)
		return llm.MarshalMessage(built)
	default:
		return nil, fmt.Errorf("%w: unsupported role %q", ErrInvalidPersistenceScenario, message.Role)
	}
}

// replayDurability applies the declared damage and records the outcomes.
func replayDurability(ctx context.Context, trace HarnessTrace, provider *sessionjsonl.Provider, root string, id persistence.SessionID, scenario PersistenceScenario) (HarnessTrace, error) {
	stat, err := provider.Stat(ctx, id)
	if err != nil {
		return HarnessTrace{}, err
	}
	trace.Lines = append(trace.Lines, fmt.Sprintf("events=%d version=%d writerHeld=%v", stat.Events, stat.FormatVersion, stat.WriterHeld))

	path, err := sessionjsonl.SessionPath(root, id.String())
	if err != nil {
		return HarnessTrace{}, err
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return HarnessTrace{}, err
	}

	if scenario.Damage == nil {
		// No damage: reopen and report the restored state.
		state, messages, err := persistence.Restore(ctx, provider, id, persistence.NewCache())
		if err != nil {
			return HarnessTrace{}, err
		}
		trace.Lines = append(trace.Lines,
			fmt.Sprintf("openTurn=%v unsettled=%v needsAttention=%v pendingInputs=%d",
				state.OpenTurn, state.UnsettledCalls, state.NeedsAttention(), state.PendingInputs))
		roles := make([]string, 0, len(messages))
		for _, message := range messages {
			roles = append(roles, string(message.Role))
		}
		trace.Lines = append(trace.Lines, "historyRoles="+strings.Join(roles, ","))
		return trace, nil
	}

	switch scenario.Damage.Kind {
	case "torn-tail":
		damaged := append(append([]byte(nil), original...), []byte(scenario.Damage.Bytes)...)
		if err := os.WriteFile(path, damaged, 0o644); err != nil {
			return HarnessTrace{}, err
		}
		analysis, err := provider.Inspect(ctx, id)
		if err != nil {
			return HarnessTrace{}, err
		}
		trace.Lines = append(trace.Lines,
			fmt.Sprintf("complete=%d repairable=%v reason=%q", analysis.Complete, analysis.Repairable, analysis.Reason))
		repaired, err := provider.Recover(ctx, id)
		if err != nil {
			return HarnessTrace{}, err
		}
		after, err := os.ReadFile(path)
		if err != nil {
			return HarnessTrace{}, err
		}
		trace.Lines = append(trace.Lines,
			fmt.Sprintf("completeAfterRepair=%d prefixUnchanged=%v", repaired.Complete, string(after) == string(original)))
	case "middle-checksum":
		lines := strings.Split(strings.TrimRight(string(original), "\n"), "\n")
		if len(lines) < 3 {
			return HarnessTrace{}, fmt.Errorf("%w: need a header and two records", ErrInvalidPersistenceScenario)
		}
		lines[2] = strings.Replace(lines[2], `"checksum":"`, `"checksum":"00`, 1)
		if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			return HarnessTrace{}, err
		}
		analysis, err := provider.Inspect(ctx, id)
		if err != nil {
			return HarnessTrace{}, err
		}
		trace.Lines = append(trace.Lines,
			fmt.Sprintf("complete=%d repairable=%v reason=%q", analysis.Complete, analysis.Repairable, analysis.Reason))
	case "version":
		header, err := sessionjsonl.EncodeHeaderAtVersion(id.String(), scenario.Damage.Version)
		if err != nil {
			return HarnessTrace{}, err
		}
		rest := original
		if index := strings.Index(string(original), "\n"); index >= 0 {
			rest = original[index+1:]
		}
		if err := os.WriteFile(path, append(append(header, '\n'), rest...), 0o644); err != nil {
			return HarnessTrace{}, err
		}
		before, err := os.ReadFile(path)
		if err != nil {
			return HarnessTrace{}, err
		}
		_, openErr := provider.Open(ctx, persistence.OpenOptions{ID: id, Access: persistence.AccessRead})
		after, err := os.ReadFile(path)
		if err != nil {
			return HarnessTrace{}, err
		}
		kind := "none"
		switch {
		case errors.Is(openErr, persistence.ErrUnsupportedVersion):
			kind = "unsupported-version"
		case errors.Is(openErr, sessionjsonl.ErrForeignFormat):
			kind = "foreign-format"
		case openErr != nil:
			kind = "other"
		}
		trace.Lines = append(trace.Lines,
			fmt.Sprintf("openRefused=%v errorKind=%s fileUnchanged=%v", openErr != nil, kind, string(before) == string(after)))
	}
	return trace, nil
}
