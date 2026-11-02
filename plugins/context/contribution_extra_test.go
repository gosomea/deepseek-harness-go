package context

import (
	"errors"
	"testing"
)

func TestValidateCoversEveryRejectionPath(t *testing.T) {
	cases := []struct {
		name         string
		contribution Contribution
	}{
		{"empty id", Contribution{Kind: KindInstruction, Source: SourceBase, Text: "x"}},
		{"time without text", Contribution{ID: "t", Kind: KindTime, Source: SourceBase}},
		{"instruction carrying a reference", Contribution{
			ID: "i", Kind: KindInstruction, Source: SourceBase, Text: "x",
			Reference: &Reference{Path: "a.md"},
		}},
		{"reference with empty path", Contribution{
			ID: "r", Kind: KindReference, Source: SourceBase,
			Reference: &Reference{Path: "   "},
		}},
		{"unknown kind", Contribution{ID: "k", Kind: Kind("mystery"), Source: SourceBase, Text: "x"}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if err := testCase.contribution.Validate(); !errors.Is(err, ErrInvalidContribution) {
				t.Fatalf("Validate error = %v, want ErrInvalidContribution", err)
			}
		})
	}
}

func TestAddRefusesAnInvalidContributionWithoutOwningIt(t *testing.T) {
	set := NewSet()
	if err := set.Add(Contribution{Kind: KindInstruction, Source: SourceBase, Text: "x"}); !errors.Is(err, ErrInvalidContribution) {
		t.Fatalf("Add error = %v, want ErrInvalidContribution", err)
	}
	if set.Len() != 0 {
		t.Fatalf("set owns %d contributions after a rejection, want 0", set.Len())
	}
}

func TestRecordRefusesAnInvalidContributionBeforeAppending(t *testing.T) {
	log := &recordingLog{}
	recorder := NewRecorder(log)
	if _, err := recorder.Record(Contribution{ID: "bad", Kind: KindInstruction, Source: SourceBase}); !errors.Is(err, ErrInvalidContribution) {
		t.Fatalf("Record error = %v, want ErrInvalidContribution", err)
	}
	if len(log.appended) != 0 {
		t.Fatalf("log saw %d appends, want none", len(log.appended))
	}
}

func TestRecordRefusesDuplicatesAgainstTheLog(t *testing.T) {
	log := &recordingLog{}
	recorder := NewRecorder(log)
	_, err := recorder.Record(
		instruction("same", SourceBase, 0, "one"),
		instruction("same", SourceSession, 0, "two"),
	)
	if !errors.Is(err, ErrDuplicateContribution) {
		t.Fatalf("Record error = %v, want ErrDuplicateContribution", err)
	}
}

func TestRebuildCitesIdenticalIDsAcrossTwoRenders(t *testing.T) {
	set := NewSet()
	for _, contribution := range []Contribution{
		instruction("a", SourceBase, 0, "one"),
		{ID: "r", Kind: KindReference, Source: SourceEnvironment, Reference: &Reference{Path: "b.md"}},
	} {
		if err := set.Add(contribution); err != nil {
			t.Fatalf("Add(%s): %v", contribution.ID, err)
		}
	}
	request, err := Rebuild(set, "")
	if err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if len(request.CitedIDs) != 2 {
		t.Fatalf("cited %d ids, want 2", len(request.CitedIDs))
	}
}
