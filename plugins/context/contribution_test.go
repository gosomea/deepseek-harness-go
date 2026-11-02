package context

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// recordingLog is the append surface the tests write through. It counts calls so a
// test can prove a rejected contribution never reached the log.
type recordingLog struct {
	appended []Contribution
	failAt   int
}

func (l *recordingLog) AppendContribution(contribution Contribution) (string, error) {
	if l.failAt > 0 && len(l.appended)+1 == l.failAt {
		return "", errors.New("log refused the append")
	}
	l.appended = append(l.appended, contribution)
	return contribution.ID, nil
}

func instruction(id string, source Source, order int, text string) Contribution {
	_ = order
	return Contribution{
		ID: id, Kind: KindInstruction, Source: source, Text: text,
		RecordedAt: time.Unix(0, 0).UTC(),
	}
}

func TestRenderIsAPureFunctionOfTheSet(t *testing.T) {
	set := NewSet()
	for _, contribution := range []Contribution{
		instruction("b", SourceSession, 0, "later"),
		instruction("a", SourceBase, 0, "first"),
	} {
		if err := set.Add(contribution); err != nil {
			t.Fatalf("Add(%s): %v", contribution.ID, err)
		}
	}
	first, err := Render(set, "")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	second, err := Render(set, "")
	if err != nil {
		t.Fatalf("Render again: %v", err)
	}
	if first.Text != second.Text {
		t.Fatalf("two renders differ: %q vs %q", first.Text, second.Text)
	}
	// Source position decides order, not insertion order.
	if got, want := first.Text, "first"+"\n\n"+"later"; got != want {
		t.Fatalf("render order = %q, want %q", got, want)
	}
	if got, want := strings.Join(first.CitedIDs, ","), "a,b"; got != want {
		t.Fatalf("cited ids = %q, want %q", got, want)
	}
}

func TestRenderRefusesANilSet(t *testing.T) {
	if _, err := Render(nil, ""); !errors.Is(err, ErrNotRecorded) {
		t.Fatalf("Render(nil) error = %v, want ErrNotRecorded", err)
	}
}

func TestEveryRenderedLineIsCited(t *testing.T) {
	set := NewSet()
	if err := set.Add(Contribution{
		ID: "ref", Kind: KindReference, Source: SourceEnvironment,
		Reference: &Reference{Path: "docs/context/contributions.md", Note: "background"},
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	request, err := Render(set, "")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(request.CitedIDs) != 1 || request.CitedIDs[0] != "ref" {
		t.Fatalf("cited ids = %v, want [ref]", request.CitedIDs)
	}
	if !strings.Contains(request.Text, "docs/context/contributions.md") {
		t.Fatalf("reference path missing from %q", request.Text)
	}
	if len(request.References) != 1 || request.References[0].Path != "docs/context/contributions.md" {
		t.Fatalf("references = %+v", request.References)
	}
}

func TestReferenceCarriesNoInlineContent(t *testing.T) {
	// A reference is a path, not the thing it points at. Inline text would make
	// the request depend on when the file was read.
	err := Contribution{
		ID: "ref", Kind: KindReference, Source: SourceEnvironment,
		Text: "leaked body", Reference: &Reference{Path: "a.md"},
	}.Validate()
	if !errors.Is(err, ErrInvalidContribution) {
		t.Fatalf("Validate error = %v, want ErrInvalidContribution", err)
	}
}

func TestInstructionRequiresText(t *testing.T) {
	err := Contribution{ID: "i", Kind: KindInstruction, Source: SourceBase, Text: "  "}.Validate()
	if !errors.Is(err, ErrInvalidContribution) {
		t.Fatalf("Validate error = %v, want ErrInvalidContribution", err)
	}
}

func TestDuplicateIDIsRefusedNotOverwritten(t *testing.T) {
	set := NewSet()
	if err := set.Add(instruction("same", SourceBase, 0, "first")); err != nil {
		t.Fatalf("first Add: %v", err)
	}
	err := set.Add(instruction("same", SourceBase, 0, "second"))
	if !errors.Is(err, ErrDuplicateContribution) {
		t.Fatalf("second Add error = %v, want ErrDuplicateContribution", err)
	}
	kept, ok := set.Get("same")
	if !ok || kept.Text != "first" {
		t.Fatalf("kept contribution = %+v, want the first one", kept)
	}
}

func TestScopeFiltersAndEmptyScopeIsVisibleEverywhere(t *testing.T) {
	set := NewSet()
	for _, contribution := range []Contribution{
		instruction("everywhere", SourceBase, 0, "all"),
		{ID: "child-only", Kind: KindInstruction, Source: SourceProfile, Text: "child", Scopes: []string{"child"}},
	} {
		if err := set.Add(contribution); err != nil {
			t.Fatalf("Add(%s): %v", contribution.ID, err)
		}
	}
	parent, err := Render(set, "parent")
	if err != nil {
		t.Fatalf("Render(parent): %v", err)
	}
	if parent.Text != "all" {
		t.Fatalf("parent render = %q, want %q", parent.Text, "all")
	}
	child, err := Render(set, "child")
	if err != nil {
		t.Fatalf("Render(child): %v", err)
	}
	if !strings.Contains(child.Text, "child") || !strings.Contains(child.Text, "all") {
		t.Fatalf("child render = %q, want both contributions", child.Text)
	}
}

func TestRecorderRecordsBeforeRendering(t *testing.T) {
	log := &recordingLog{}
	recorder := NewRecorder(log)
	set, err := recorder.Record(
		instruction("a", SourceBase, 0, "one"),
		instruction("b", SourceSession, 0, "two"),
	)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if len(log.appended) != 2 {
		t.Fatalf("log saw %d contributions, want 2", len(log.appended))
	}
	if set.Len() != 2 {
		t.Fatalf("set owns %d contributions, want 2", set.Len())
	}
}

func TestRecorderDropsWhatTheLogRefused(t *testing.T) {
	log := &recordingLog{failAt: 2}
	recorder := NewRecorder(log)
	set, err := recorder.Record(
		instruction("a", SourceBase, 0, "one"),
		instruction("b", SourceSession, 0, "two"),
	)
	if err == nil {
		t.Fatal("Record succeeded although the log refused the second append")
	}
	if set != nil {
		t.Fatalf("set = %+v, want nil when the log refused", set)
	}
	// The refused contribution must not be citable through any later render.
	if _, ok := NewSet().Get("b"); ok {
		t.Fatal("a refused contribution became reachable")
	}
}

func TestRenderFailsLoudlyOnAnUnknownKind(t *testing.T) {
	// Reach the renderer the way only a future kind added without a render rule
	// could: by inserting past validation. A silent skip would drop content from
	// the request with no diagnostic.
	set := NewSet()
	set.byID["ghost"] = Contribution{ID: "ghost", Kind: Kind("future"), Source: SourceBase}
	set.order = append(set.order, "ghost")
	_, err := Render(set, "")
	if !errors.Is(err, ErrNotRecorded) {
		t.Fatalf("Render error = %v, want ErrNotRecorded", err)
	}
}

func TestRebuildDetectsAgreement(t *testing.T) {
	set := NewSet()
	if err := set.Add(instruction("a", SourceBase, 0, "one")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	request, err := Rebuild(set, "")
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if request.Text != "one" {
		t.Fatalf("Rebuild text = %q, want %q", request.Text, "one")
	}
}

func TestRebuildRefusesANilSet(t *testing.T) {
	if _, err := Rebuild(nil, ""); !errors.Is(err, ErrNotRecorded) {
		t.Fatalf("Rebuild(nil) error = %v, want ErrNotRecorded", err)
	}
}

func TestOrderedIsStableAcrossRepeatedCalls(t *testing.T) {
	set := NewSet()
	for _, id := range []string{"c", "a", "b"} {
		if err := set.Add(instruction(id, SourceProfile, 0, id)); err != nil {
			t.Fatalf("Add(%s): %v", id, err)
		}
	}
	first := set.Ordered("")
	second := set.Ordered("")
	if len(first) != len(second) {
		t.Fatalf("lengths differ: %d vs %d", len(first), len(second))
	}
	for index := range first {
		if first[index].ID != second[index].ID {
			t.Fatalf("position %d differs: %s vs %s", index, first[index].ID, second[index].ID)
		}
	}
	if ids := strings.Join(set.IDs(), ","); ids != "c,a,b" {
		t.Fatalf("IDs() = %q, want insertion order c,a,b", ids)
	}
}

func TestRecorderWithoutLogFails(t *testing.T) {
	if _, err := NewRecorder(nil).Record(instruction("a", SourceBase, 0, "x")); !errors.Is(err, ErrNotRecorded) {
		t.Fatalf("Record error = %v, want ErrNotRecorded", err)
	}
	var nilRecorder *Recorder
	if _, err := nilRecorder.Record(instruction("a", SourceBase, 0, "x")); !errors.Is(err, ErrNotRecorded) {
		t.Fatalf("nil Recorder error = %v, want ErrNotRecorded", err)
	}
}

func TestUnknownSourceIsRefused(t *testing.T) {
	err := instruction("i", Source("mystery"), 0, "text").Validate()
	if !errors.Is(err, ErrInvalidContribution) {
		t.Fatalf("Validate error = %v, want ErrInvalidContribution", err)
	}
}
