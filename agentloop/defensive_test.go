package agentloop_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gosomea/deepseek-harness-go/agent"
	"github.com/gosomea/deepseek-harness-go/agentloop"
	"github.com/gosomea/deepseek-harness-go/internal/testkit"
	"github.com/gosomea/deepseek-harness-go/llm"
	"github.com/gosomea/deepseek-harness-go/session"
)

// A context cancelled before the turn starts is refused without writing anything.
func TestRunRefusesAnAlreadyCancelledContext(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"unused"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "go"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	runner, _ := agentloop.AsRunner(handle)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runner.RunTurn(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if got := len(provider.Attempts()); got != 0 {
		t.Fatalf("a cancelled run must not call the provider, got %d", got)
	}
	// The turn boundary is opened and the input recorded before the first step
	// checks the context, so those facts stay in the log: a cancelled run must not
	// erase what already happened. What must not appear is a step or a request.
	for _, eventType := range eventTypes(log) {
		if eventType == "step/start" || eventType == "request/header" || eventType == "assistant/attempt" {
			t.Fatalf("a cancelled run must not start a step, got %v", eventTypes(log))
		}
	}
	if !contains(strings.Join(eventTypes(log), ","), "turn/end") {
		t.Fatalf("the cancelled turn must close, got %v", eventTypes(log))
	}
}

// A run on a disposed Agent is refused rather than reviving it.
func TestRunOnADisposedAgentIsRefused(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"unused"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := owner.Dispose(context.Background()); err != nil {
		t.Fatalf("dispose: %v", err)
	}
	// Input after disposal is refused, so the run has nothing to claim; a direct
	// run on the disposed instance must still be refused rather than opening a
	// turn for a dead Agent.
	runner, ok := agentloop.AsRunner(handle)
	if !ok {
		t.Fatalf("handle must be runnable, got %T", handle)
	}
	if err := runner.RunTurn(context.Background()); err != nil {
		t.Fatalf("a run with no input must be a no-op, got %v", err)
	}
	if got := log.Len(); got != 0 {
		t.Fatalf("a disposed Agent must write nothing, got %d events", got)
	}
}

// Wait honours a cancelled context while a turn is in flight, rather than
// blocking past the caller's deadline.
func TestWaitHonoursCancellationWhileActive(t *testing.T) {
	log := newLog(t)
	provider := newBlockingProvider()
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "go"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	runner, _ := agentloop.AsRunner(handle)
	turnDone := make(chan error, 1)
	go func() { turnDone <- runner.RunTurn(context.Background()) }()
	<-provider.started

	ctx, cancel := context.WithCancel(context.Background())
	waitErr := make(chan error, 1)
	go func() { waitErr <- handle.Wait(ctx) }()
	cancel()
	select {
	case err := <-waitErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled from Wait, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not honour the cancelled context")
	}
	// Cancel the turn so the goroutine finishes before the test ends.
	if canceler, ok := handle.(agent.Canceler); ok {
		_, _ = canceler.Cancel(context.Background())
	}
	select {
	case <-turnDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the turn never returned")
	}
}

// Cancel honours a cancelled context of its own.
func TestCancelHonoursItsOwnContext(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"ok"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	canceler, ok := owner.Handle().(agent.Canceler)
	if !ok {
		t.Fatalf("handle must implement Canceler, got %T", owner.Handle())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := canceler.Cancel(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

// An input refused by the inbox is reported, not silently dropped.
func TestInboxRefusesInputAfterClosing(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"ok"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	// Closing happens as part of disposal; the refusal is observed through Send.
	if err := owner.Dispose(context.Background()); err != nil {
		t.Fatalf("dispose: %v", err)
	}
	if err := handle.Send(context.Background(), agent.Input{Text: "after"}); !errors.Is(err, agent.ErrInputRejected) {
		t.Fatalf("want ErrInputRejected, got %v", err)
	}
}

// A creation attempt on a closed factory is refused before any work.
func TestCreateOnAClosedFactoryIsRefused(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"ok"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	if err := factory.DisposeAll(context.Background()); err != nil {
		t.Fatalf("dispose all: %v", err)
	}
	if _, err := factory.Create(context.Background(), spec()); !errors.Is(err, agent.ErrClosed) {
		t.Fatalf("want ErrClosed, got %v", err)
	}
}

// An initial input that the inbox refuses fails creation rather than publishing
// an Agent that silently lost its first input.
func TestInitialInputThatCannotBeQueuedFailsCreation(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"ok"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	request := spec()
	blank := agent.Input{Text: " "}
	request.InitialInput = &blank
	if _, err := factory.Create(context.Background(), request); !errors.Is(err, agent.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue, got %v", err)
	}
	if got := len(factory.Active()); got != 0 {
		t.Fatalf("a failed creation must publish nothing, got %d", got)
	}
}

// A creation whose context is already cancelled fails without publishing.
func TestCreateRefusesACancelledContext(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"ok"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := factory.Create(ctx, spec()); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if got := len(factory.Active()); got != 0 {
		t.Fatalf("a cancelled creation must publish nothing, got %d", got)
	}
}

// A turn whose cause is a non-cancellation error reports that cause rather than
// a bare context.Canceled, so the caller learns why the turn stopped.
func TestTurnReportsANonCancellationCause(t *testing.T) {
	log := newLog(t)
	provider := newBlockingProvider()
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "go"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	// A context whose cancellation carries a distinct cause: the driver must
	// surface that cause instead of the plain cancellation.
	cause := errors.New("supervisor aborted the turn")
	ctx, cancel := context.WithCancelCause(context.Background())
	runner, _ := agentloop.AsRunner(handle)
	turnDone := make(chan error, 1)
	go func() { turnDone <- runner.RunTurn(ctx) }()
	<-provider.started
	cancel(cause)
	err = <-turnDone
	if err == nil {
		t.Fatal("an aborted turn must report an error")
	}
	if !errors.Is(err, cause) {
		t.Fatalf("want the cause %v, got %v", cause, err)
	}
}

// The event payload encoder is a general helper, so an unencodable value is a
// reachable input: it must fail loudly rather than write a corrupt event.
func TestEncodeRejectsAnUnencodablePayload(t *testing.T) {
	// A channel cannot be marshalled; the helper must panic rather than emit an
	// invalid event, and the panic must say what could not be encoded.
	defer func() {
		reason := recover()
		if reason == nil {
			t.Fatal("encoding an unmarshalable payload must panic")
		}
	}()
	agentloop.EncodePayloadForTest(make(chan int))
}

// The nested-payload helpers are general too, and must report an unencodable
// payload instead of returning a half-built event.
func TestNestedEncodeRejectsAnUnencodablePayload(t *testing.T) {
	if _, err := agentloop.EncodeNestedForTest(make(chan int), []byte(`{}`)); err == nil {
		t.Fatal("encoding an unmarshalable payload must fail")
	}
	if _, err := agentloop.EncodeNestedForTest(struct{ X int }{X: 1}, []byte(`{`)); err == nil {
		t.Fatal("an invalid message must fail")
	}
	encoded, err := agentloop.EncodeNestedForTest(struct{ X int }{X: 1}, []byte(`{"a":1}`))
	if err != nil {
		t.Fatalf("a valid payload must encode: %v", err)
	}
	if !contains(string(encoded), `"message":{"a":1}`) {
		t.Fatalf("the nested message was not attached: %s", encoded)
	}
}

// A turn whose step bound is reached reports the bound and still closes.
func TestStepLimitIsReportedAndTheTurnCloses(t *testing.T) {
	log := newLog(t)
	var bodyRuns int
	scripts := make([][]llm.StreamChunk, 0, 3)
	for index := 0; index < 3; index++ {
		scripts = append(scripts, testkit.ScriptToolCallAttempt("call-1", "read_file", []string{`{"path":"a.txt"}`}))
	}
	provider := testkit.NewFakeProvider(scripts...)
	factory := newFactory(t, agentloop.Config{
		Provider: provider, Log: log, Pipeline: readFileTool(t, &bodyRuns), MaxSteps: 1,
	})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "loop"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := run(handle); !errors.Is(err, agentloop.ErrStepLimit) {
		t.Fatalf("want ErrStepLimit, got %v", err)
	}
	types := eventTypes(log)
	if types[len(types)-1] != "turn/end" {
		t.Fatalf("the bounded turn must close, got %v", types)
	}
	if !contains(string(log.Events()[len(log.Events())-1].Data), `"failed":true`) {
		t.Fatal("hitting the bound must be recorded as a failed turn")
	}
}

// Every write failure in the turn path must abort the turn rather than continue
// with a log that no longer explains the work.
//
// The registry below accepts the boundary events and the request header but
// refuses the assistant attempt, so the failure happens after the request was
// sent, which is the case a partial log would hide.
func TestTurnAbortsWhenAnAttemptCannotBeRecorded(t *testing.T) {
	registry := session.NewRegistry("turn/start", "user/message", "step/start", "request/header")
	log := session.NewLog(registry)
	if err := log.SetClock(func() time.Time { return fixedTime }); err != nil {
		t.Fatalf("set clock: %v", err)
	}
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"answer"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "go"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := run(handle); !errors.Is(err, agentloop.ErrEventRejected) {
		t.Fatalf("want ErrEventRejected, got %v", err)
	}
	// The request was sent, but no assistant message may be recorded from an
	// attempt the log could not describe.
	for _, event := range log.Events() {
		if event.Type == "assistant/message" {
			t.Fatal("an unrecordable attempt must not produce a recorded message")
		}
	}
}

// A step boundary that cannot be recorded aborts the turn before any request.
func TestTurnAbortsWhenAStepBoundaryCannotBeRecorded(t *testing.T) {
	registry := session.NewRegistry("turn/start", "user/message")
	log := session.NewLog(registry)
	if err := log.SetClock(func() time.Time { return fixedTime }); err != nil {
		t.Fatalf("set clock: %v", err)
	}
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"never sent"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "go"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := run(handle); !errors.Is(err, agentloop.ErrEventRejected) {
		t.Fatalf("want ErrEventRejected, got %v", err)
	}
	if got := len(provider.Attempts()); got != 0 {
		t.Fatalf("no request may be sent when the step cannot be recorded, got %d", got)
	}
}

// A tool call that cannot be recorded aborts the batch before the tool runs, so
// a side effect never happens without its record.
func TestTurnAbortsWhenAToolCallCannotBeRecorded(t *testing.T) {
	registry := session.NewRegistry(
		"turn/start", "user/message", "step/start", "request/header",
		"assistant/attempt", "assistant/message", "step/end", "turn/end",
	)
	log := session.NewLog(registry)
	if err := log.SetClock(func() time.Time { return fixedTime }); err != nil {
		t.Fatalf("set clock: %v", err)
	}
	var bodyRuns int
	provider := testkit.NewFakeProvider(
		testkit.ScriptToolCallAttempt("call-1", "read_file", []string{`{"path":"a.txt"}`}),
		testkit.ScriptTextAttempt([]string{"done"}),
	)
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log, Pipeline: readFileTool(t, &bodyRuns)})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "read"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if err := run(handle); !errors.Is(err, agentloop.ErrEventRejected) {
		t.Fatalf("want ErrEventRejected, got %v", err)
	}
	if bodyRuns != 0 {
		t.Fatalf("the tool must not run when its call cannot be recorded, ran %d times", bodyRuns)
	}
}

// Input queued before the turn starts is claimed as one batch, so the model sees
// it in one request rather than one request per input. This is the reference's
// "claim pending next-step input plus one queued prompt": a turn is not started
// per message.
func TestPreQueuedInputsAreClaimedAsOneBatch(t *testing.T) {
	log := newLog(t)
	provider := testkit.NewFakeProvider(testkit.ScriptTextAttempt([]string{"one answer for both"}))
	factory := newFactory(t, agentloop.Config{Provider: provider, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	for _, text := range []string{"one", "two"} {
		if err := handle.Send(context.Background(), agent.Input{Text: text}); err != nil {
			t.Fatalf("send %s: %v", text, err)
		}
	}
	if err := run(handle); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := len(provider.Attempts()); got != 1 {
		t.Fatalf("pre-queued inputs belong to one request, got %d requests", got)
	}
	if got := len(provider.Attempts()[0].Messages); got != 2 {
		t.Fatalf("the one request must carry both inputs, got %d messages", got)
	}
	turns, steps := 0, 0
	for _, eventType := range eventTypes(log) {
		switch eventType {
		case "turn/start":
			turns++
		case "step/start":
			steps++
		}
	}
	if turns != 1 || steps != 1 {
		t.Fatalf("want 1 turn and 1 step, got %d turns and %d steps", turns, steps)
	}
}

// Input that arrives while a turn is in flight is claimed by the next step of the
// same turn, which is what makes steering observable rather than racy.
func TestSteeringDuringATurnAddsAStep(t *testing.T) {
	log := newLog(t)
	provider := newBlockingProvider()
	// The second script answers whatever the next step asks.
	steered := testkit.NewFakeProvider()
	_ = steered
	factory := newFactory(t, agentloop.Config{Provider: &steeringProvider{blocking: provider}, Log: log})
	owner, err := factory.Create(context.Background(), spec())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	handle := owner.Handle()
	if err := handle.Send(context.Background(), agent.Input{Text: "start"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	runner, _ := agentloop.AsRunner(handle)
	turnDone := make(chan error, 1)
	go func() { turnDone <- runner.RunTurn(context.Background()) }()
	<-provider.started
	// Steering arrives while the first step is still in flight.
	if err := handle.Send(context.Background(), agent.Input{Text: "also do this"}); err != nil {
		t.Fatalf("steer: %v", err)
	}
	close(provider.release)
	if err := <-turnDone; err != nil {
		t.Fatalf("run: %v", err)
	}
	steps := 0
	for _, eventType := range eventTypes(log) {
		if eventType == "step/start" {
			steps++
		}
	}
	if steps != 2 {
		t.Fatalf("steering must add a step in the same turn, got %d steps: %v", steps, eventTypes(log))
	}
	turns := 0
	for _, eventType := range eventTypes(log) {
		if eventType == "turn/start" {
			turns++
		}
	}
	if turns != 1 {
		t.Fatalf("steering must not start a second turn, got %d", turns)
	}
}

// steeringProvider answers the first attempt with the blocking stream and later
// attempts with a fixed text answer, so a steered second step can complete.
type steeringProvider struct {
	blocking *blockingProvider
	calls    int
}

func (p *steeringProvider) Info() llm.ProviderInfo {
	return llm.ProviderInfo{ID: "steering", Name: "Steering", Capabilities: []llm.Capability{llm.CapabilityStreaming}}
}

func (p *steeringProvider) Generate(ctx context.Context, request llm.Request) (llm.Stream, error) {
	p.calls++
	if p.calls == 1 {
		return p.blocking.Generate(ctx, request)
	}
	return testkit.NewFakeStream(testkit.ScriptTextAttempt([]string{"steered answer"})), nil
}
