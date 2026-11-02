package sessionjsonl

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosomea/deepseek-harness-go/persistence"
	"github.com/gosomea/deepseek-harness-go/session"
)

// fixedTime keeps recorded timestamps identical between runs.
var fixedTime = time.UnixMilli(1_700_000_000_000)

// newProvider returns a provider over a fresh temporary directory.
func newProvider(t *testing.T) *Provider {
	t.Helper()
	provider, err := New(Options{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	return provider
}

// appendEvent writes one event through a handle.
func appendEvent(t *testing.T, handle persistence.Handle, eventType string, data string) session.Event {
	t.Helper()
	at := fixedTime
	event, err := handle.Append(context.Background(), session.AppendInput{
		Type: eventType, Data: []byte(data), Time: &at,
	})
	if err != nil {
		t.Fatalf("append %s: %v", eventType, err)
	}
	return event
}

func createSession(t *testing.T, provider *Provider, id persistence.SessionID) persistence.Stat {
	t.Helper()
	stat, err := provider.Create(context.Background(), persistence.CreateOptions{ID: id})
	if err != nil {
		t.Fatalf("create %s: %v", id, err)
	}
	return stat
}

func openWrite(t *testing.T, provider *Provider, id persistence.SessionID) persistence.Handle {
	t.Helper()
	handle, err := provider.Open(context.Background(), persistence.OpenOptions{ID: id, Access: persistence.AccessWrite})
	if err != nil {
		t.Fatalf("open write %s: %v", id, err)
	}
	return handle
}

func openRead(t *testing.T, provider *Provider, id persistence.SessionID) persistence.Handle {
	t.Helper()
	handle, err := provider.Open(context.Background(), persistence.OpenOptions{ID: id, Access: persistence.AccessRead})
	if err != nil {
		t.Fatalf("open read %s: %v", id, err)
	}
	return handle
}

// A created session is empty, has this build's version, and is listed.
func TestCreateStatAndList(t *testing.T) {
	provider := newProvider(t)
	stat := createSession(t, provider, "s-1")
	if stat.ID != "s-1" || stat.FormatVersion != FormatVersion || stat.Events != 0 {
		t.Fatalf("unexpected stat %+v", stat)
	}
	if stat.RecoverableTail {
		t.Fatal("a fresh session has no torn tail")
	}
	if _, err := provider.Create(context.Background(), persistence.CreateOptions{ID: "s-1"}); !errors.Is(err, persistence.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a duplicate create, got %v", err)
	}
	if _, err := provider.Create(context.Background(), persistence.CreateOptions{}); !errors.Is(err, persistence.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without an id, got %v", err)
	}
	createSession(t, provider, "a-2")
	stats, err := provider.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(stats) != 2 || stats[0].ID != "a-2" || stats[1].ID != "s-1" {
		t.Fatalf("List must be sorted by id, got %+v", stats)
	}
	if _, err := provider.Stat(context.Background(), "absent"); !errors.Is(err, persistence.ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound, got %v", err)
	}
	if _, err := provider.Stat(context.Background(), ""); !errors.Is(err, persistence.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue for a blank id, got %v", err)
	}
}

// Appends are visible to the writer's own reads immediately, and survive a
// reopen once flushed.
func TestAppendFlushAndReopen(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	handle := openWrite(t, provider, "s-1")
	first := appendEvent(t, handle, "turn/start", `{"turn":0}`)
	second := appendEvent(t, handle, "turn/end", `{"turn":0}`)
	if first.Seq != 0 || second.Seq != 1 {
		t.Fatalf("sequences must be dense, got %d and %d", first.Seq, second.Seq)
	}
	// The writer reads its own appends before any flush.
	visible, err := handle.Read(context.Background(), 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(visible) != 2 {
		t.Fatalf("a write handle must read its own appends, got %d", len(visible))
	}
	if err := handle.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// A fresh provider sees the flushed prefix.
	reopened, err := New(Options{DataDir: provider.DataDir()})
	if err != nil {
		t.Fatalf("reopen provider: %v", err)
	}
	reader := openRead(t, reopened, "s-1")
	events, err := reader.Read(context.Background(), 0)
	if err != nil {
		t.Fatalf("read after reopen: %v", err)
	}
	if len(events) != 2 || events[0].Type != "turn/start" || events[1].Type != "turn/end" {
		t.Fatalf("reopened history differs: %+v", events)
	}
	// Reading from an offset returns the suffix.
	tail, err := reader.Read(context.Background(), 1)
	if err != nil {
		t.Fatalf("read from 1: %v", err)
	}
	if len(tail) != 1 || tail[0].Seq != 1 {
		t.Fatalf("unexpected suffix %+v", tail)
	}
	// Reading past the end is empty, not an error.
	past, err := reader.Read(context.Background(), 9)
	if err != nil || len(past) != 0 {
		t.Fatalf("reading past the end: events=%v err=%v", past, err)
	}
}

// A second writer is refused, both in-process and through the on-disk lease.
func TestSecondWriterIsRefused(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	first := openWrite(t, provider, "s-1")
	if _, err := provider.Open(context.Background(), persistence.OpenOptions{ID: "s-1", Access: persistence.AccessWrite}); !errors.Is(err, persistence.ErrWriterExists) {
		t.Fatalf("want ErrWriterExists for a second writer, got %v", err)
	}
	// A read handle is allowed while a writer holds the session.
	reader := openRead(t, provider, "s-1")
	if _, err := reader.Read(context.Background(), 0); err != nil {
		t.Fatalf("a reader must work while a writer holds the session: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close reader: %v", err)
	}
	// Closing the writer releases both the claim and the lease.
	if err := first.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	if _, err := os.Stat(leaseFileFor(provider.DataDir(), "s-1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the writer lease must be removed on close, got %v", err)
	}
	second := openWrite(t, provider, "s-1")
	if err := second.Close(); err != nil {
		t.Fatalf("close second: %v", err)
	}
}

// The on-disk lease is what makes the rule hold across processes, so a lease left
// behind by another process refuses a writer even in a fresh provider.
func TestWriterLeaseSurvivesAcrossProviders(t *testing.T) {
	first := newProvider(t)
	createSession(t, first, "s-1")
	handle := openWrite(t, first, "s-1")

	second, err := New(Options{DataDir: first.DataDir()})
	if err != nil {
		t.Fatalf("second provider: %v", err)
	}
	if _, err := second.Open(context.Background(), persistence.OpenOptions{ID: "s-1", Access: persistence.AccessWrite}); err == nil {
		t.Fatal("a second process must not be able to write while the lease exists")
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	third := openWrite(t, second, "s-1")
	if err := third.Close(); err != nil {
		t.Fatalf("close third: %v", err)
	}
}

// A read handle cannot append, and a closed handle refuses everything.
func TestReadOnlyAndClosedRules(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	reader := openRead(t, provider, "s-1")
	if _, err := reader.Append(context.Background(), session.AppendInput{Type: "turn/start", Data: []byte(`{}`)}); !errors.Is(err, persistence.ErrReadOnly) {
		t.Fatalf("want ErrReadOnly, got %v", err)
	}
	if err := reader.Flush(context.Background()); err != nil {
		t.Fatalf("a read handle may flush as a no-op: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("second close must be a no-op, got %v", err)
	}
	if _, err := reader.Read(context.Background(), 0); !errors.Is(err, persistence.ErrHandleClosed) {
		t.Fatalf("want ErrHandleClosed, got %v", err)
	}

	writer := openWrite(t, provider, "s-1")
	if err := writer.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := writer.Append(context.Background(), session.AppendInput{Type: "turn/start", Data: []byte(`{}`)}); !errors.Is(err, persistence.ErrHandleClosed) {
		t.Fatalf("want ErrHandleClosed after close, got %v", err)
	}
	if err := writer.Flush(context.Background()); !errors.Is(err, persistence.ErrHandleClosed) {
		t.Fatalf("want ErrHandleClosed from flush after close, got %v", err)
	}
}

// A cancelled context refuses the work rather than performing it.
func TestCancelledContextRefusesWork(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	handle := openWrite(t, provider, "s-1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := handle.Append(ctx, session.AppendInput{Type: "turn/start", Data: []byte(`{}`)}); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled from append, got %v", err)
	}
	if err := handle.Flush(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled from flush, got %v", err)
	}
	if _, err := handle.Read(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled from read, got %v", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// A failed append writes nothing: the artifact is either extended by one complete
// record or left untouched.
func TestFailedAppendWritesNothing(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	handle := openWrite(t, provider, "s-1")
	appendEvent(t, handle, "turn/start", `{"turn":0}`)
	// Flush first, so the size below is the committed artifact and not a buffer
	// that a later flush would grow regardless.
	if err := handle.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	before, err := os.Stat(handlePath(t, provider, "s-1"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if _, err := handle.Append(context.Background(), session.AppendInput{Type: "", Data: []byte(`{}`)}); err == nil {
		t.Fatal("an event without a type must be refused")
	}
	if _, err := handle.Append(context.Background(), session.AppendInput{Type: "turn/start"}); err == nil {
		t.Fatal("an event without data must be refused")
	}
	if err := handle.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	after, err := os.Stat(handlePath(t, provider, "s-1"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if after.Size() != before.Size() {
		t.Fatalf("a refused append must not change the artifact: %d -> %d", before.Size(), after.Size())
	}
}

// Concurrent appends through one handle stay dense and all survive.
func TestConcurrentAppendsStayDense(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	handle := openWrite(t, provider, "s-1")
	const writers = 8
	const perWriter = 5
	var group sync.WaitGroup
	// A barrier releases every writer at once, so the test exercises real
	// contention rather than hoping for it.
	start := make(chan struct{})
	for index := 0; index < writers; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			for count := 0; count < perWriter; count++ {
				if _, err := handle.Append(context.Background(), session.AppendInput{
					Type: "user/message",
					Data: []byte(`{"id":"m","role":"user","content":[{"type":"text","text":"x"}],"source":{"kind":"user"}}`),
					// A surface-eligible event must declare its surface op; the
					// log refuses one that does not.
					SurfaceOp: &session.SurfaceOp{Op: session.SurfaceAppend},
				}); err != nil {
					t.Errorf("append: %v", err)
					return
				}
			}
		}()
	}
	close(start)
	group.Wait()
	if err := handle.Flush(context.Background()); err != nil {
		t.Fatalf("flush: %v", err)
	}
	events, err := handle.Read(context.Background(), 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(events) != writers*perWriter {
		t.Fatalf("want %d events, got %d", writers*perWriter, len(events))
	}
	for index, event := range events {
		if event.Seq != session.Seq(index) {
			t.Fatalf("sequence %d is %d", index, event.Seq)
		}
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// Closing the provider while a writer is open is refused: dropping the
// bookkeeping would let another process claim a session still being written.
func TestCloseRefusesWhileAWriterIsOpen(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	handle := openWrite(t, provider, "s-1")
	if err := provider.Close(); !errors.Is(err, persistence.ErrWriterExists) {
		t.Fatalf("want ErrWriterExists, got %v", err)
	}
	if err := handle.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("close provider: %v", err)
	}
	if _, err := provider.Open(context.Background(), persistence.OpenOptions{ID: "s-1", Access: persistence.AccessRead}); !errors.Is(err, persistence.ErrHandleClosed) {
		t.Fatalf("want ErrHandleClosed after closing the service, got %v", err)
	}
}

// A handle for a session that does not exist, or with an invalid mode, is
// refused before anything is acquired.
func TestOpenRefusals(t *testing.T) {
	provider := newProvider(t)
	if _, err := provider.Open(context.Background(), persistence.OpenOptions{ID: "absent", Access: persistence.AccessRead}); !errors.Is(err, persistence.ErrSessionNotFound) {
		t.Fatalf("want ErrSessionNotFound, got %v", err)
	}
	createSession(t, provider, "s-1")
	if _, err := provider.Open(context.Background(), persistence.OpenOptions{ID: "s-1"}); !errors.Is(err, persistence.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without an access mode, got %v", err)
	}
	if _, err := provider.Open(context.Background(), persistence.OpenOptions{Access: persistence.AccessRead}); !errors.Is(err, persistence.ErrInvalidValue) {
		t.Fatalf("want ErrInvalidValue without an id, got %v", err)
	}
	if _, err := New(Options{}); !errors.Is(err, ErrDataDirMissing) {
		t.Fatalf("want ErrDataDirMissing, got %v", err)
	}
}

// handlePath returns the artifact path for assertions.
func handlePath(t *testing.T, provider *Provider, id persistence.SessionID) string {
	t.Helper()
	path, err := SessionPath(provider.DataDir(), id.String())
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	return path
}

// The artifact declares this project's own identity rather than borrowing the
// reference implementation's.
//
// The assertion names this project's identifier and checks that the header is the
// only declaration; it deliberately does not spell out the reference's generation
// name, because a literal of that name in the tree is exactly what a boundary
// check is there to catch.
func TestArtifactKeepsItsOwnIdentity(t *testing.T) {
	provider := newProvider(t)
	createSession(t, provider, "s-1")
	path := handlePath(t, provider, "s-1")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(body), FormatID) {
		t.Fatalf("the header must declare this project's format: %s", body)
	}
	// The name is the artifact's own: it carries the format identity in the
	// header rather than in the filename, so a foreign name cannot appear.
	if strings.Contains(filepath.Base(path), FormatID) {
		t.Fatalf("the filename must not need the format id: %s", path)
	}
	if strings.Contains(strings.ToLower(string(body)), "v4") {
		t.Fatalf("the artifact must not claim a foreign generation: %s", body)
	}
}
