package scope

import (
	"errors"
	"strings"
	"testing"
)

// TestNamedEntriesKeepsInsertionOrderAndRejectsDuplicates pins the two properties
// a registry built on this table depends on: presented order is registration
// order, and a duplicate inside one layer is a conflict rather than a shadow.
func TestNamedEntriesKeepsInsertionOrderAndRejectsDuplicates(t *testing.T) {
	table := NewNamedEntries[string](nil)
	for _, name := range []string{"b", "a", "c"} {
		if _, err := table.Insert(name, name+"-value"); err != nil {
			t.Fatalf("Insert(%s) error = %v", name, err)
		}
	}
	if got := table.Names(); strings.Join(got, ",") != "b,a,c" {
		t.Fatalf("Names() = %v, want insertion order", got)
	}
	if _, err := table.Insert("a", "again"); !errors.Is(err, ErrDuplicateEntry) {
		t.Fatalf("Insert(duplicate) = %v, want ErrDuplicateEntry", err)
	}
	if value, ok := table.Get("a"); !ok || value != "a-value" {
		t.Fatalf("Get(a) = %q/%t, want the original value kept", value, ok)
	}
	if !table.Has("b") || table.Has("z") {
		t.Fatal("Has() did not report membership correctly")
	}
	if table.IsEmpty() {
		t.Fatal("IsEmpty() = true for a populated table")
	}
	if entries := table.Entries(); len(entries) != 3 || entries[2].Name != "c" {
		t.Fatalf("Entries() = %v, want three ordered pairs", entries)
	}
}

// TestNamedEntriesUndoRemovesOnlyItsOwnEntry pins that disposing one contribution
// cannot remove another's, and that the undo is idempotent.
func TestNamedEntriesUndoRemovesOnlyItsOwnEntry(t *testing.T) {
	table := NewNamedEntries[string](nil)
	undoA, err := table.Insert("a", "A")
	if err != nil {
		t.Fatalf("Insert(a) error = %v", err)
	}
	if _, err := table.Insert("b", "B"); err != nil {
		t.Fatalf("Insert(b) error = %v", err)
	}
	undoA()
	undoA() // idempotent
	if table.Has("a") {
		t.Fatal("the undone entry survived")
	}
	if !table.Has("b") {
		t.Fatal("the undo removed a different entry")
	}
	if got := table.Names(); len(got) != 1 || got[0] != "b" {
		t.Fatalf("Names() = %v, want only b", got)
	}
	// A name can be re-registered after its undo, and lands at the end.
	if _, err := table.Insert("a", "A2"); err != nil {
		t.Fatalf("re-Insert(a) error = %v", err)
	}
	if got := table.Names(); len(got) != 2 || got[1] != "a" {
		t.Fatalf("Names() = %v, want the re-registered name last", got)
	}
	if table.IsEmpty() {
		t.Fatal("IsEmpty() = true after re-registration")
	}
}

// TestNamedEntriesUsesCallerDiagnostic pins that the duplicate message belongs to
// the registry, not to this generic table.
func TestNamedEntriesUsesCallerDiagnostic(t *testing.T) {
	table := NewNamedEntries[string](func(name string) error {
		return errors.New("tool " + name + " is already registered")
	})
	if _, err := table.Insert("read", "v"); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}
	_, err := table.Insert("read", "v")
	if err == nil || !strings.Contains(err.Error(), "tool read is already registered") {
		t.Fatalf("Insert(duplicate) = %v, want the caller's diagnostic", err)
	}
}

// TestAnonymousEntriesTreatEqualValuesAsSeparate pins the identity rule a table of
// guards needs: two identical registrations are two contributions with independent undos.
func TestAnonymousEntriesTreatEqualValuesAsSeparate(t *testing.T) {
	table := NewAnonymousEntries[string]()
	undoFirst := table.Append("same")
	undoSecond := table.Append("same")
	if table.Len() != 2 {
		t.Fatalf("Len() = %d, want two independent registrations", table.Len())
	}
	if got := table.Values(); len(got) != 2 || got[0] != "same" || got[1] != "same" {
		t.Fatalf("Values() = %v, want both registrations", got)
	}
	undoFirst()
	undoFirst() // idempotent
	if table.Len() != 1 {
		t.Fatalf("Len() after one undo = %d, want 1", table.Len())
	}
	if table.IsEmpty() {
		t.Fatal("IsEmpty() = true while one registration remains")
	}
	undoSecond()
	if !table.IsEmpty() || table.Len() != 0 {
		t.Fatal("the table is not empty after both undos")
	}
	if got := table.Values(); len(got) != 0 {
		t.Fatalf("Values() = %v, want none", got)
	}
}
