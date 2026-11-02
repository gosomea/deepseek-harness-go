package clientview

import (
	"errors"
	"fmt"
	"sort"
)

// Errors this package returns.
var (
	// ErrGap reports a request whose last-seen sequence is not contiguous with
	// what the host can serve, so a full snapshot is required.
	ErrGap = errors.New("clientview: sequence gap requires a snapshot")
)

// Entry is one client-visible entry, keyed by sequence.
//
// It is a projection, not the internal session event: the client sees a stable id,
// a sequence and rendered text, never a session.Event by reference.
type Entry struct {
	// Seq is the entry's sequence in the authoritative log.
	Seq int
	// ID is the entry identity.
	ID string
	// Text is the rendered text a client shows.
	Text string
}

// Snapshot is the full view a client receives on connect.
type Snapshot struct {
	// ThroughSeq is the last sequence included.
	ThroughSeq int
	// Entries are the entries up to ThroughSeq, in order.
	Entries []Entry
}

// Delta is the incremental view after a sequence.
type Delta struct {
	// FromSeq is the first sequence after the client's last-seen.
	FromSeq int
	// Entries are the entries after FromSeq-1, in order.
	Entries []Entry
}

// BuildSnapshot returns the snapshot covering entries up to throughSeq.
//
// It validates ordering first: a snapshot built from out-of-order entries would
// produce a client view that disagrees with the log, so an unordered input is an
// error rather than a silently sorted result.
func BuildSnapshot(entries []Entry, throughSeq int) (Snapshot, error) {
	if err := checkOrdered(entries); err != nil {
		return Snapshot{}, err
	}
	var kept []Entry
	for _, entry := range entries {
		if entry.Seq <= throughSeq {
			kept = append(kept, entry)
		}
	}
	return Snapshot{ThroughSeq: throughSeq, Entries: kept}, nil
}

// BuildDelta returns the entries strictly after fromSeq.
//
// fromSeq is the client's last-seen sequence. If the log does not contain entries
// contiguous from fromSeq+1 (there is a gap because older entries were dropped),
// it returns ErrGap so the caller asks for a snapshot instead of showing a
// silently incomplete view.
func BuildDelta(entries []Entry, fromSeq int) (Delta, error) {
	if err := checkOrdered(entries); err != nil {
		return Delta{}, err
	}
	var after []Entry
	for _, entry := range entries {
		if entry.Seq > fromSeq {
			after = append(after, entry)
		}
	}
	// If the log's earliest available entry is later than fromSeq+1, there is a
	// gap the client cannot fill incrementally.
	if len(entries) > 0 && entries[0].Seq > fromSeq+1 {
		return Delta{}, fmt.Errorf("%w: earliest %d, requested after %d", ErrGap, entries[0].Seq, fromSeq)
	}
	return Delta{FromSeq: fromSeq + 1, Entries: after}, nil
}

// checkOrdered verifies entries are strictly increasing in sequence.
func checkOrdered(entries []Entry) error {
	if !sort.SliceIsSorted(entries, func(i, j int) bool { return entries[i].Seq < entries[j].Seq }) {
		return fmt.Errorf("clientview: entries are not ordered by sequence")
	}
	return nil
}
