package testkit_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/capability/fs"
	"github.com/gosomea/deepseek-harness-go/capability/workspace"
	"github.com/gosomea/deepseek-harness-go/internal/testkit"
	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/persistence"
	"github.com/gosomea/deepseek-harness-go/plugins/local"
	"github.com/gosomea/deepseek-harness-go/session"
)

// A CLI scenario whose step kind is unknown is refused during replay as well as
// during validation, so a caller that skipped Validate still cannot half-run it.
func TestCLIScenarioReplayRefusesAnInvalidStep(t *testing.T) {
	scenario := testkit.CLIScenario{
		ID:    "invalid-step",
		Title: "未知步骤",
		Steps: []testkit.CLIStep{{Kind: "tool", Name: "read_file"}},
	}
	// A tool step without a call id is still replayable (the pipeline refuses it),
	// so this one only checks that validation gates the unknown kind.
	scenario.Steps[0].Kind = "invented"
	if _, err := testkit.ReplayCLIScenario(scenario); err == nil {
		t.Fatal("an unknown step kind must be refused")
	}
}

// A CLI scenario whose tool step is missing its name cannot be validated, so a
// caller that built one by hand is told rather than silently skipping the step.
func TestCLIScenarioValidatesSteps(t *testing.T) {
	for name, scenario := range map[string]testkit.CLIScenario{
		"seed without a path": {ID: "a", Steps: []testkit.CLIStep{{Kind: "seed"}}},
		"tool without a name": {ID: "b", Steps: []testkit.CLIStep{{Kind: "tool"}}},
	} {
		if err := scenario.Validate(); err == nil {
			t.Fatalf("%s must be refused", name)
		}
	}
}

// Loading a persistence scenario that is structurally valid JSON but not a
// scenario is refused.
func TestPersistenceScenarioLoadRejectsAnEmptyDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := testkit.LoadPersistenceScenario(path); err == nil {
		t.Fatal("a document without an id must be refused")
	}
	broken := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(broken, []byte("{"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := testkit.LoadPersistenceScenario(broken); err == nil {
		t.Fatal("broken JSON must be refused")
	}
}

// A persistence scenario whose event carries an unreadable payload is refused
// during replay, which is where the fold would otherwise fail obscurely.
func TestPersistenceScenarioRefusesAnUnreadableEvent(t *testing.T) {
	scenario := testkit.PersistenceScenario{
		ID:    "unreadable-event",
		Title: "无法解码的事件",
		Events: []testkit.ScenarioEvent{
			{Type: "user/message", Data: map[string]any{"not": "a message"}, Surface: "append"},
		},
	}
	if _, err := testkit.ReplayPersistenceScenario(scenario); err == nil {
		t.Fatal("an unreadable event must be reported")
	}
}

// Every apply-option the fake provider exposes must actually change its
// behaviour, or it is a knob that lies.
func TestFakeProviderOptionsTakeEffect(t *testing.T) {
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"hi"}))
	caps := provider.Info().Capabilities
	if len(caps) == 0 {
		t.Fatal("the fake provider must declare the capabilities it keeps")
	}
	// The close-error option makes Close report the configured failure.
	closeErr := errors.New("scripted close failure")
	closing := testkit.NewFakeProviderWithOptions(testkit.FakeProviderCloseError(closeErr))
	if err := closing.Close(); !errors.Is(err, closeErr) {
		t.Fatalf("want the scripted close failure, got %v", err)
	}
	// A provider with declared capabilities reports exactly those. The options
	// constructor takes only options; scripts are supplied to NewFakeProvider.
	limited := testkit.NewFakeProviderWithOptions(testkit.FakeProviderCapabilities(llm.CapabilityStreaming))
	if got := limited.Info().Capabilities; len(got) != 1 || got[0] != llm.CapabilityStreaming {
		t.Fatalf("unexpected capabilities %v", got)
	}
	// The default provider is usable: it streams what the script says.
	stream, err := provider.Generate(context.Background(), llm.Request{Provider: "fake", Model: "fake-1"})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	defer func() { _ = stream.Close() }()
	// A scripted attempt opens with its block-start chunk before the text delta, so
	// the assertion drains the stream and looks for the text rather than assuming
	// the first chunk carries it.
	var text strings.Builder
	for {
		chunk, ok, err := stream.Recv()
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		if !ok {
			break
		}
		text.WriteString(chunk.Text)
	}
	if text.String() != "hi" {
		t.Fatalf("the scripted answer must be streamed, got %q", text.String())
	}
}

// The fake store's failure seams report their own call and no other.
func TestFakeStoreFailureSeams(t *testing.T) {
	store := testkit.NewFakeStore()
	store.Seed("s-1", session.Event{Type: "turn/start", Data: []byte(`{"turn":0}`)})
	boom := errors.New("scripted")
	store.FailStat(boom)
	if _, err := store.Stat(context.Background(), "s-1"); !errors.Is(err, boom) {
		t.Fatalf("want the stat failure, got %v", err)
	}
	// The success path reports the recorded count, which is the branch a healthy
	// call takes.
	store.FailStat(nil)
	stat, err := store.Stat(context.Background(), "s-1")
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if stat.Events != 1 || stat.FormatVersion == 0 || stat.ID != "s-1" {
		t.Fatalf("unexpected stat %+v", stat)
	}
}

// The local filesystem provider is reachable through the capability seam, which
// is what the scenario replayers rely on.
func TestLocalProviderSatisfiesTheSeam(t *testing.T) {
	root := t.TempDir()
	world, err := workspace.New(workspace.Options{ID: "w", Root: root})
	if err != nil {
		t.Fatalf("workspace: %v", err)
	}
	provider, err := local.NewFS(local.FSConfig{World: world, Mode: fs.ModeWorkspaceWrite})
	if err != nil {
		t.Fatalf("fs: %v", err)
	}
	var seam fs.Service = provider
	if _, err := seam.Write(local.AsCtx(context.Background()), fs.WriteRequest{Path: "a.txt", Content: []byte("x")}); err != nil {
		t.Fatalf("write through the seam: %v", err)
	}
	read, err := seam.Read(local.AsCtx(context.Background()), "a.txt")
	if err != nil {
		t.Fatalf("read through the seam: %v", err)
	}
	if strings.TrimSpace(string(read.Content)) != "x" {
		t.Fatalf("unexpected content %q", read.Content)
	}
}

// A persistence scenario whose damage kind is unknown is refused before replay,
// so a typo cannot silently skip the corruption it meant to apply.
func TestPersistenceScenarioRefusesAnUnknownDamageKind(t *testing.T) {
	scenario := testkit.PersistenceScenario{
		ID:     "unknown-damage",
		Title:  "未知损坏类型",
		Events: []testkit.ScenarioEvent{{Type: "turn/start", Data: map[string]any{"turn": 0}}},
		Damage: &testkit.ScenarioDamage{Kind: "invented"},
	}
	if err := scenario.Validate(); err == nil {
		t.Fatal("an unknown damage kind must be refused")
	}
	if _, err := testkit.ReplayPersistenceScenario(scenario); err == nil {
		t.Fatal("replay must refuse it too")
	}
}

// The scenario directories are reported as paths under a repository root, which
// is how a caller locates the fixtures.
func TestScenarioDirectoriesResolveUnderARoot(t *testing.T) {
	if got := testkit.PersistenceScenarioDir("/repo"); !strings.HasSuffix(got, filepath.Join("testdata", "parity", "persistence", "scenarios")) {
		t.Fatalf("unexpected persistence path %q", got)
	}
	if got := testkit.HarnessScenarioDir("/repo"); !strings.HasSuffix(got, filepath.Join("testdata", "parity", "agentloop", "scenarios")) {
		t.Fatalf("unexpected harness path %q", got)
	}
	// A stat on a missing session is reported, which the CLI's ensureSession uses.
	store := testkit.NewFakeStore()
	if _, err := store.Stat(context.Background(), "absent"); !errors.Is(err, persistence.ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound, got %v", err)
	}
}
