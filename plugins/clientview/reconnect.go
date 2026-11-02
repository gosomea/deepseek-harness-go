package clientview

// Hub serves snapshots and deltas from a fixed ordered entry slice.
//
// It models the host side of a reconnect: a client that stayed online receives
// deltas; a client that fell behind asks for a snapshot. The hub is a pure view
// over entries, so tests drive reconnect semantics without a socket.
type Hub struct {
	entries []Entry
}

// NewHub returns a hub over ordered entries.
func NewHub(entries []Entry) (*Hub, error) {
	if err := checkOrdered(entries); err != nil {
		return nil, err
	}
	return &Hub{entries: append([]Entry(nil), entries...)}, nil
}

// Latest returns the snapshot through the newest entry.
func (h *Hub) Latest() Snapshot {
	through := 0
	if len(h.entries) > 0 {
		through = h.entries[len(h.entries)-1].Seq
	}
	snapshot, _ := BuildSnapshot(h.entries, through)
	return snapshot
}

// Since returns a delta after a client's last-seen sequence, or ErrGap when the
// client fell behind what the hub still holds.
func (h *Hub) Since(fromSeq int) (Delta, error) {
	return BuildDelta(h.entries, fromSeq)
}

// Resume reconstructs a view for a client that last saw fromSeq.
//
// It prefers an incremental delta and falls back to a full snapshot when there is
// a gap, so a reconnecting client never sees an incomplete view. It reports which
// path was taken so a caller can log whether the client had to resync.
func (h *Hub) Resume(fromSeq int) (View, bool, error) {
	if delta, err := h.Since(fromSeq); err == nil {
		base, err := BuildSnapshot(h.entries, fromSeq)
		if err != nil {
			return View{}, false, err
		}
		view, err := ApplySnapshot(base).ApplyDelta(delta)
		if err != nil {
			return View{}, false, err
		}
		return view, false, nil
	}
	return ApplySnapshot(h.Latest()), true, nil
}
