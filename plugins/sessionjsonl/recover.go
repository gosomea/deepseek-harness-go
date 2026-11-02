package sessionjsonl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/gosomea/deepseek-harness-go/persistence"
	"github.com/gosomea/deepseek-harness-go/session"
)

// Analysis describes what recovery found in one artifact.
type Analysis struct {
	// ID is the session identity read from the header.
	ID persistence.SessionID
	// FormatVersion is the version the header declares.
	FormatVersion int
	// Complete counts the complete records.
	Complete int
	// TornTailBytes is how many trailing bytes are an incomplete record, or 0.
	TornTailBytes int64
	// Repairable reports that the only damage is a torn tail.
	Repairable bool
	// Reason explains why a damaged artifact is not repairable, when it is not.
	Reason string
}

// Inspect reports what one artifact contains without modifying it.
//
// It is the diagnostic a caller runs before deciding to repair: the same check
// that recovery uses, exposed so a reader can explain what it found.
func (p *Provider) Inspect(ctx context.Context, id persistence.SessionID) (Analysis, error) {
	if id.IsZero() {
		return Analysis{}, fmt.Errorf("%w: inspect requires a session id", persistence.ErrInvalidValue)
	}
	if err := ctx.Err(); err != nil {
		return Analysis{}, err
	}
	path, err := SessionPath(p.dataDir, id.String())
	if err != nil {
		return Analysis{}, err
	}
	return inspectArtifact(path)
}

// inspectArtifact walks one file and classifies its condition.
func inspectArtifact(path string) (Analysis, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Analysis{}, fmt.Errorf("%w: %s", persistence.ErrSessionNotFound, path)
		}
		return Analysis{}, err
	}
	defer func() { _ = file.Close() }()

	reader := bufio.NewReaderSize(file, 64*1024)
	headerLine, err := readLine(reader)
	if err != nil {
		return Analysis{}, fmt.Errorf("%w: %v", ErrInvalidHeader, err)
	}
	parsed, err := decodeHeader(headerLine)
	if err != nil {
		return Analysis{}, err
	}
	analysis := Analysis{ID: persistence.SessionID(parsed.ID), FormatVersion: parsed.Version}

	var offset int64
	offset += int64(len(headerLine)) + 1
	for {
		start := offset
		line, err := readLine(reader)
		offset += int64(len(line))
		if err == nil {
			offset++
		}
		if errors.Is(err, io.EOF) {
			return analysis, nil
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			// The final line has no terminator: the writer stopped mid-record.
			analysis.TornTailBytes = offset - start
			analysis.Repairable = true
			return analysis, nil
		}
		if err != nil {
			return Analysis{}, err
		}
		if strings.TrimSpace(string(line)) == "" {
			analysis.Reason = fmt.Sprintf("blank record at offset %d", analysis.Complete)
			return analysis, nil
		}
		decoded, err := decodeRecord(line)
		if err != nil {
			// A damaged record that is not at the very end is unrecoverable: the
			// records after it cannot be placed, and the ones before it no longer
			// form a contiguous log once the hole is dropped.
			analysis.Reason = fmt.Sprintf("corrupt record at offset %d: %v", analysis.Complete, err)
			return analysis, nil
		}
		if decoded.Seq != uint64(analysis.Complete) {
			analysis.Reason = fmt.Sprintf("record at offset %d declares seq %d", analysis.Complete, decoded.Seq)
			return analysis, nil
		}
		if _, err := session.UnmarshalEvent(decoded.Payload); err != nil {
			analysis.Reason = fmt.Sprintf("unreadable event at offset %d: %v", analysis.Complete, err)
			return analysis, nil
		}
		analysis.Complete++
	}
}

// Recover repairs a torn tail, returning the analysis it acted on.
//
// Only a torn tail is repaired: the incomplete bytes are dropped and the
// committed prefix is left byte-for-byte identical. Damage anywhere else is
// refused, because a repair that dropped a middle region would silently remove
// records the caller cannot get back.
func (p *Provider) Recover(ctx context.Context, id persistence.SessionID) (Analysis, error) {
	if id.IsZero() {
		return Analysis{}, fmt.Errorf("%w: recover requires a session id", persistence.ErrInvalidValue)
	}
	if err := ctx.Err(); err != nil {
		return Analysis{}, err
	}
	if p.service.WriterHeld(id) {
		return Analysis{}, fmt.Errorf("%w: %s", persistence.ErrWriterExists, id)
	}
	path, err := SessionPath(p.dataDir, id.String())
	if err != nil {
		return Analysis{}, err
	}
	analysis, err := inspectArtifact(path)
	if err != nil {
		return Analysis{}, err
	}
	if analysis.TornTailBytes == 0 && analysis.Reason == "" {
		return analysis, nil
	}
	if analysis.Reason != "" {
		return analysis, fmt.Errorf("%w: %s: %s", persistence.ErrCorruptReport, id, analysis.Reason)
	}

	info, err := os.Stat(path)
	if err != nil {
		return Analysis{}, err
	}
	keep := info.Size() - analysis.TornTailBytes
	if keep < 0 {
		return Analysis{}, fmt.Errorf("%w: %s: torn length exceeds file size", persistence.ErrCorruptReport, id)
	}
	// Dropping a torn tail is a truncation to the last complete record. A
	// temp-file-and-rename sequence would be needed only if the kept region moved,
	// and it does not: truncation removes bytes from the end and leaves every
	// committed byte at the same offset, so the prefix stays byte-for-byte
	// identical without a second copy.
	if err := os.Truncate(path, keep); err != nil {
		return Analysis{}, err
	}
	// Re-inspect the repaired artifact instead of editing the earlier result by
	// hand: the returned analysis must describe what is actually on disk now.
	return inspectArtifact(path)
}

// InspectArtifactAt reports what one artifact at an arbitrary path contains.
//
// It exists so a diagnostic can examine a copy — the file a reader salvaged
// after an incident, say — without first placing it where a provider would look
// for a session. It reports; it never modifies.
func InspectArtifactAt(path string) (Analysis, error) {
	return inspectArtifact(path)
}
