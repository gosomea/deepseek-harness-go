package clientview

// View is a rebuildable client view.
type View struct {
	// Entries are the currently shown entries, in order.
	Entries []Entry
	// ThroughSeq is the last applied sequence.
	ThroughSeq int
}

// ApplySnapshot starts a view from a snapshot.
func ApplySnapshot(snapshot Snapshot) View {
	return View{Entries: append([]Entry(nil), snapshot.Entries...), ThroughSeq: snapshot.ThroughSeq}
}

// ApplyDelta applies a delta to a view, returning the new view.
//
// It refuses a delta that does not start exactly at the view's last sequence+1:
// silently applying a mismatched delta would duplicate or skip entries, which is
// the failure reconnect exists to prevent.
func (v View) ApplyDelta(delta Delta) (View, error) {
	if delta.FromSeq != v.ThroughSeq+1 {
		return v, ErrGap
	}
	entries := append([]Entry(nil), v.Entries...)
	through := v.ThroughSeq
	for _, entry := range delta.Entries {
		entries = append(entries, entry)
		through = entry.Seq
	}
	return View{Entries: entries, ThroughSeq: through}, nil
}

// SameView reports whether two views show identical entries and sequences.
func SameView(a, b View) bool {
	if a.ThroughSeq != b.ThroughSeq || len(a.Entries) != len(b.Entries) {
		return false
	}
	for i := range a.Entries {
		if a.Entries[i] != b.Entries[i] {
			return false
		}
	}
	return true
}
