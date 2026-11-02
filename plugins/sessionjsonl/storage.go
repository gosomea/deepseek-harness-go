package sessionjsonl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gosomea/deepseek-harness-go/persistence"
	"github.com/gosomea/deepseek-harness-go/session"
)

// Options configures a JSONL provider.
type Options struct {
	// DataDir is the directory artifacts live in. Required.
	//
	// The provider writes only under this directory; it never touches a user's
	// existing DSH home.
	DataDir string
}

// Provider is the JSONL implementation of persistence.Service.
type Provider struct {
	// service carries the writer bookkeeping shared by every backend.
	service persistence.BaseService
	// dataDir is the configured data directory.
	dataDir string
	// mu guards the per-session locks that serialize appends within a process.
	mu sync.Mutex
	// locks holds one mutex per session id held for writing.
	locks map[persistence.SessionID]*sync.Mutex
}

// New returns a provider writing under the configured directory.
//
// The directory is created if absent, because a provider that failed on a
// missing directory would force every caller to prepare storage it does not
// own.
func New(options Options) (*Provider, error) {
	if strings.TrimSpace(options.DataDir) == "" {
		return nil, ErrDataDirMissing
	}
	if err := os.MkdirAll(options.DataDir, 0o755); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidValue, err)
	}
	return &Provider{
		service: persistence.NewBaseService(),
		dataDir: options.DataDir,
		locks:   map[persistence.SessionID]*sync.Mutex{},
	}, nil
}

// DataDir reports the directory this provider writes under.
func (p *Provider) DataDir() string { return p.dataDir }

// Create makes a new session, refusing an id that already exists.
//
// The artifact is created with the header first, so a crash during creation
// leaves either nothing or a valid empty session — never a file another reader
// would misinterpret.
func (p *Provider) Create(ctx context.Context, options persistence.CreateOptions) (persistence.Stat, error) {
	if err := persistence.ValidateCreate(options); err != nil {
		return persistence.Stat{}, err
	}
	if err := ctx.Err(); err != nil {
		return persistence.Stat{}, err
	}
	path, err := SessionPath(p.dataDir, options.ID.String())
	if err != nil {
		return persistence.Stat{}, err
	}
	if _, err := os.Stat(path); err == nil {
		return persistence.Stat{}, fmt.Errorf("%w: %s already exists", persistence.ErrInvalidValue, options.ID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return persistence.Stat{}, err
	}
	encoded, err := EncodeHeader(options.ID.String())
	if err != nil {
		return persistence.Stat{}, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return persistence.Stat{}, err
	}
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		_ = file.Close()
		return persistence.Stat{}, err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return persistence.Stat{}, err
	}
	if err := file.Close(); err != nil {
		return persistence.Stat{}, err
	}
	return p.Stat(ctx, options.ID)
}

// Open returns a handle with the requested access.
//
// A write open claims the session's single writer slot before touching the file,
// so a second writer is refused without acquiring anything. A read open takes no
// claim and works while a writer holds the session.
func (p *Provider) Open(ctx context.Context, options persistence.OpenOptions) (persistence.Handle, error) {
	if err := options.Validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// A closed service refuses every open, read or write: the writer bookkeeping
	// is gone, so a write open could not enforce its single-writer rule.
	if p.service.Closed() {
		return nil, fmt.Errorf("%w: service is closed", persistence.ErrHandleClosed)
	}
	path, err := SessionPath(p.dataDir, options.ID.String())
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", persistence.ErrSessionNotFound, options.ID)
		}
		return nil, err
	}
	// The header is read before the claim so an unreadable artifact fails as a
	// format problem rather than as a session someone else owns.
	if _, err := readHeader(path); err != nil {
		return nil, err
	}
	handle := &jsonlHandle{id: options.ID, path: path, provider: p}
	handle.Init(options.ID, options.Access)
	if options.Access == persistence.AccessWrite {
		// The in-process claim is taken first, then the on-disk lease. The order
		// matters: a lease without a claim would let two handles in one process
		// fight over the file, and a claim without a lease would let a second
		// process in.
		if err := p.service.ClaimWriter(options.ID); err != nil {
			return nil, err
		}
		lease, err := claimLease(p.dataDir, options.ID.String())
		if err != nil {
			p.service.ReleaseWriter(options.ID)
			return nil, err
		}
		handle.leasePath = lease
		handle.ownsWriter = true
		p.mu.Lock()
		if p.locks[options.ID] == nil {
			p.locks[options.ID] = &sync.Mutex{}
		}
		handle.lock = p.locks[options.ID]
		p.mu.Unlock()
		file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			p.service.ReleaseWriter(options.ID)
			return nil, err
		}
		handle.file = file
		handle.writer = bufio.NewWriter(file)
	}
	return handle, nil
}

// Stat describes a session without opening it.
func (p *Provider) Stat(ctx context.Context, id persistence.SessionID) (persistence.Stat, error) {
	if id.IsZero() {
		return persistence.Stat{}, fmt.Errorf("%w: stat requires a session id", persistence.ErrInvalidValue)
	}
	if err := ctx.Err(); err != nil {
		return persistence.Stat{}, err
	}
	path, err := SessionPath(p.dataDir, id.String())
	if err != nil {
		return persistence.Stat{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return persistence.Stat{}, fmt.Errorf("%w: %s", persistence.ErrSessionNotFound, id)
		}
		return persistence.Stat{}, err
	}
	parsed, err := readHeader(path)
	if err != nil {
		return persistence.Stat{}, err
	}
	events, tail, err := countRecords(path)
	if err != nil {
		return persistence.Stat{}, err
	}
	return persistence.Stat{
		ID:              id,
		FormatVersion:   parsed.Version,
		Events:          events,
		Bytes:           info.Size(),
		RecoverableTail: tail,
		WriterHeld:      p.service.WriterHeld(id),
	}, nil
}

// List returns every session in the directory, sorted by id.
func (p *Provider) List(ctx context.Context) ([]persistence.Stat, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(p.dataDir)
	if err != nil {
		return nil, err
	}
	stats := make([]persistence.Stat, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), LogSuffix) {
			continue
		}
		path := filepath.Join(p.dataDir, entry.Name())
		parsed, err := readHeader(path)
		if err != nil {
			// A file this build cannot read is reported rather than skipped: a
			// silently shortened listing would hide a version problem.
			return nil, err
		}
		events, tail, err := countRecords(path)
		if err != nil {
			return nil, err
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		stats = append(stats, persistence.Stat{
			ID:              persistence.SessionID(parsed.ID),
			FormatVersion:   parsed.Version,
			Events:          events,
			Bytes:           info.Size(),
			RecoverableTail: tail,
			WriterHeld:      p.service.WriterHeld(persistence.SessionID(parsed.ID)),
		})
	}
	persistence.SortStats(stats)
	return stats, nil
}

// Close releases the provider and refuses later use.
//
// It refuses to close while a writer is still open: dropping the bookkeeping
// would let a second process claim the session while its first handle still
// holds an open file.
func (p *Provider) Close() error {
	_, held := p.service.MarkClosed()
	if len(held) > 0 {
		return fmt.Errorf("%w: %d writer(s) still open", persistence.ErrWriterExists, len(held))
	}
	return nil
}

// jsonlHandle is one open channel onto a JSONL artifact.
type jsonlHandle struct {
	// HandleBase carries the shared closed and read-only rules.
	persistence.HandleBase
	// id and path identify the artifact.
	id   persistence.SessionID
	path string
	// provider is the backend this handle belongs to.
	provider *Provider
	// file is the append handle, present only for a write handle.
	file *os.File
	// writer buffers appends until a flush.
	writer *bufio.Writer
	// lock serializes appends within this process.
	lock *sync.Mutex
	// ownsWriter records that this handle holds the session's writer claim.
	ownsWriter bool
	// leasePath is the on-disk writer lease this handle created.
	leasePath string
	// mu guards the in-memory view used to serve this handle's own reads.
	mu sync.Mutex
	// events caches the records this handle has observed so far.
	events []session.Event
	// loaded records whether the cache was filled from disk.
	loaded bool
}

// Read returns the committed events from the given offset.
func (h *jsonlHandle) Read(ctx context.Context, from session.Seq) ([]session.Event, error) {
	if err := h.HandleBase.CheckOpen(false); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.loaded {
		events, _, err := loadEvents(h.path)
		if err != nil {
			return nil, err
		}
		h.events = events
		h.loaded = true
	}
	if int(from) >= len(h.events) {
		return nil, nil
	}
	return append([]session.Event(nil), h.events[from:]...), nil
}

// Append commits one event at the end of the log.
func (h *jsonlHandle) Append(ctx context.Context, input session.AppendInput) (session.Event, error) {
	if err := h.HandleBase.CheckOpen(true); err != nil {
		return session.Event{}, err
	}
	if err := ctx.Err(); err != nil {
		return session.Event{}, err
	}
	if h.lock != nil {
		h.lock.Lock()
		defer h.lock.Unlock()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.loaded {
		events, _, err := loadEvents(h.path)
		if err != nil {
			return session.Event{}, err
		}
		h.events = events
		h.loaded = true
	}
	event, err := buildEvent(session.Seq(len(h.events)), input)
	if err != nil {
		return session.Event{}, err
	}
	payload, err := session.MarshalEvent(event)
	if err != nil {
		return session.Event{}, err
	}
	line, err := EncodeRecord(uint64(event.Seq), payload)
	if err != nil {
		return session.Event{}, err
	}
	if _, err := h.writer.Write(append(line, '\n')); err != nil {
		return session.Event{}, err
	}
	// The append is visible to this handle's own reads as soon as it is buffered,
	// which is exactly the weaker promise Append makes; durability waits for
	// Flush.
	h.events = append(h.events, event)
	return event, nil
}

// Flush makes every completed append durable.
func (h *jsonlHandle) Flush(ctx context.Context) error {
	if err := h.HandleBase.CheckOpen(false); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if h.writer == nil {
		return nil
	}
	if err := h.writer.Flush(); err != nil {
		return err
	}
	if err := h.file.Sync(); err != nil {
		return err
	}
	return nil
}

// Close releases the handle, flushing and releasing the writer claim.
//
// It is idempotent. A close that cannot flush reports the failure rather than
// pretending the data is safe.
func (h *jsonlHandle) Close() error {
	if h.HandleBase.Closed() {
		return nil
	}
	var errs []error
	if h.writer != nil {
		if err := h.writer.Flush(); err != nil {
			errs = append(errs, err)
		}
		if err := h.file.Sync(); err != nil {
			errs = append(errs, err)
		}
		if err := h.file.Close(); err != nil {
			errs = append(errs, err)
		}
		h.writer = nil
	}
	_ = h.HandleBase.Close()
	if h.ownsWriter {
		if err := releaseLease(h.leasePath); err != nil {
			errs = append(errs, err)
		}
		h.provider.service.ReleaseWriter(h.id)
	}
	return errors.Join(errs...)
}

// buildEvent turns an append input into the event to store.
//
// It mirrors the log's own rules: the sequence is the current length, so a gap
// is impossible by construction, and the payload must be present.
func buildEvent(seq session.Seq, input session.AppendInput) (session.Event, error) {
	if input.Type == "" {
		return session.Event{}, fmt.Errorf("%w: event requires a type", persistence.ErrInvalidValue)
	}
	if len(input.Data) == 0 {
		return session.Event{}, fmt.Errorf("%w: event requires data", persistence.ErrInvalidValue)
	}
	time := session.TimeMS(0)
	if input.Time != nil {
		time = session.TimeMS(input.Time.UnixMilli())
	}
	event := session.Event{
		Type:            input.Type,
		Seq:             seq,
		Time:            time,
		Data:            append([]byte(nil), input.Data...),
		Ignorable:       session.Ignorable(input.Ignorable),
		HasIgnorable:    input.Ignorable,
		SourceEventSeqs: append([]session.Seq(nil), input.SourceEventSeqs...),
	}
	if input.SurfaceOp != nil {
		op := *input.SurfaceOp
		event.SurfaceOp = &op
	}
	if err := session.ValidateEventEnvelope(event); err != nil {
		return session.Event{}, err
	}
	return event, nil
}

// readHeader reads and validates the header line of an artifact.
func readHeader(path string) (header, error) {
	file, err := os.Open(path)
	if err != nil {
		return header{}, err
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	if !scanner.Scan() {
		return header{}, fmt.Errorf("%w: artifact is empty", ErrInvalidHeader)
	}
	return decodeHeader(scanner.Bytes())
}

// countRecords counts complete records and reports whether the tail is torn.
func countRecords(path string) (int, bool, error) {
	events, tail, err := loadEvents(path)
	if err != nil {
		return 0, false, err
	}
	return len(events), tail, nil
}

// loadEvents reads every complete record from an artifact.
//
// A torn final line is reported through the boolean rather than as an error: it
// is a recoverable condition, and the caller decides whether to repair. Damage
// anywhere else is an error, because the missing records cannot be recovered.
func loadEvents(path string) ([]session.Event, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = file.Close() }()
	reader := bufio.NewReaderSize(file, 64*1024)
	// The first line is the header.
	headerLine, err := readLine(reader)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %v", ErrInvalidHeader, err)
	}
	if _, err := decodeHeader(headerLine); err != nil {
		return nil, false, err
	}
	var events []session.Event
	for {
		line, err := readLine(reader)
		if errors.Is(err, io.EOF) {
			// A clean end of file after a newline is not a torn tail.
			return events, false, nil
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			// The final line has no terminator, so the writer stopped mid-record.
			// That is a recoverable tail and not an error: Stat must be able to
			// report it, and Recover exists to drop it.
			return events, true, nil
		}
		if err != nil {
			return nil, false, err
		}
		if len(strings.TrimSpace(string(line))) == 0 {
			// A blank line in the middle means a record was lost rather than
			// never written.
			return nil, false, fmt.Errorf("%w: blank record at offset %d", ErrCorruptRecord, len(events))
		}
		decoded, err := decodeRecord(line)
		if err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) {
				return events, true, nil
			}
			return nil, false, err
		}
		if decoded.Seq != uint64(len(events)) {
			return nil, false, fmt.Errorf("%w: record at offset %d declares seq %d",
				ErrCorruptRecord, len(events), decoded.Seq)
		}
		event, err := session.UnmarshalEvent(decoded.Payload)
		if err != nil {
			return nil, false, fmt.Errorf("%w: %v", ErrCorruptRecord, err)
		}
		events = append(events, event)
	}
}

// readLine reads one record line, distinguishing a torn tail from a clean end.
//
// A final line without a terminating newline is torn: the writer may have been
// interrupted mid-record, and treating it as complete would accept a partial
// payload.
func readLine(reader *bufio.Reader) ([]byte, error) {
	line, err := reader.ReadBytes('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			if len(line) == 0 {
				return nil, io.EOF
			}
			return line, io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return line[:len(line)-1], nil
}
