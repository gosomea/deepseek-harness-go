package loader_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/loader"
)

// TestNewCatalogIsEmpty keeps a fresh catalog from claiming any plugin.
func TestNewCatalogIsEmpty(t *testing.T) {
	catalog := loader.NewCatalog()
	if names := catalog.Names(); len(names) != 0 {
		t.Fatalf("a new catalog should be empty, got %v", names)
	}
	if catalog.Has("alpha") {
		t.Fatal("a new catalog must not report an unregistered plugin")
	}
}

// TestCatalogRejectsEmptyNameAndNilFactory keeps registration mistakes observable
// instead of producing an entry that fails later for an unrelated reason.
func TestCatalogRejectsEmptyNameAndNilFactory(t *testing.T) {
	catalog := loader.NewCatalog()
	if err := catalog.Register("", recordingPlugin("", nil)); !errors.Is(err, loader.ErrInvalidConfig) {
		t.Fatalf("empty name: %v", err)
	}
	if err := catalog.Register("alpha", nil); !errors.Is(err, loader.ErrInvalidConfig) {
		t.Fatalf("nil factory: %v", err)
	}
	if catalog.Has("alpha") {
		t.Fatal("a rejected registration must not be stored")
	}
}

// TestCatalogLookupUnknownAndReplace pins lookup failure and hot replacement.
func TestCatalogLookupUnknownAndReplace(t *testing.T) {
	catalog := mustCatalog(t, "alpha")
	if _, err := catalog.Lookup("beta"); !errors.Is(err, loader.ErrUnknownPlugin) {
		t.Fatalf("unknown lookup: %v", err)
	}
	if !catalog.Has("alpha") {
		t.Fatal("alpha should be registered")
	}
	replacement := recordingPlugin("alpha", nil)
	if err := catalog.Register("alpha", replacement); err != nil {
		t.Fatalf("replacing a registration should succeed: %v", err)
	}
	got, err := catalog.Lookup("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("replacement factory is nil")
	}
	if names := catalog.Names(); len(names) != 1 || names[0] != "alpha" {
		t.Fatalf("Names = %v, want [alpha]", names)
	}
}

// TestCatalogNamesAreSorted keeps configuration dumps deterministic.
func TestCatalogNamesAreSorted(t *testing.T) {
	catalog := mustCatalog(t, "zeta", "alpha", "mid")
	got := catalog.Names()
	want := []string{"alpha", "mid", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("Names = %v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("Names = %v, want %v", got, want)
		}
	}
}

// TestParseDocumentRejectsNonObjectAndBadJSON covers decoder-level rejections.
func TestParseDocumentRejectsNonObjectAndBadJSON(t *testing.T) {
	catalog := loader.NewCatalog()
	for _, doc := range []string{`[]`, `null`, `{`, `{"entries":[{"id":"a","name":"n","inject":{}}]}`} {
		if _, err := loader.Load([]byte(doc), catalog); !errors.Is(err, loader.ErrInvalidConfig) {
			t.Fatalf("document %s: %v", doc, err)
		}
	}
}

// TestInjectAcceptsStringArrayAndNull pins the three accepted forms of inject.
func TestInjectAcceptsStringArrayAndNull(t *testing.T) {
	catalog := mustCatalog(t, "alpha")
	for _, tc := range []struct {
		doc  string
		want []string
	}{
		{`{"entries":[{"id":"a","name":"alpha"}]}`, nil},
		{`{"entries":[{"id":"a","name":"alpha","inject":"store"}]}`, []string{"store"}},
		{`{"entries":[{"id":"a","name":"alpha","inject":["store","clock"]}]}`, []string{"store", "clock"}},
		{`{"entries":[{"id":"a","name":"alpha","inject":null}]}`, nil},
	} {
		entries, err := loader.Load([]byte(tc.doc), catalog)
		if err != nil {
			t.Fatalf("%s: %v", tc.doc, err)
		}
		got := entries[0].Options().Inject
		if len(got) != len(tc.want) {
			t.Fatalf("%s: inject = %v, want %v", tc.doc, got, tc.want)
		}
		for index := range tc.want {
			if got[index] != tc.want[index] {
				t.Fatalf("%s: inject = %v, want %v", tc.doc, got, tc.want)
			}
		}
	}
}

// TestInjectRejectsEmptyNames keeps a blank dependency from becoming a silent
// unmatched requirement later.
func TestInjectRejectsEmptyNames(t *testing.T) {
	catalog := mustCatalog(t, "alpha")
	for _, doc := range []string{
		`{"entries":[{"id":"a","name":"alpha","inject":""}]}`,
		`{"entries":[{"id":"a","name":"alpha","inject":["store",""]}]}`,
	} {
		if _, err := loader.Load([]byte(doc), catalog); !errors.Is(err, loader.ErrInvalidConfig) {
			t.Fatalf("document %s: %v", doc, err)
		}
	}
}

// TestTypeErrorNamesTheField keeps the diagnostic actionable: a wrong type must
// point at the offending field rather than a generic decode failure.
func TestTypeErrorNamesTheField(t *testing.T) {
	catalog := loader.NewCatalog()
	_, err := loader.Load([]byte(`{"entries":[{"id":"a","name":"alpha","config":1}],"extra":1}`), catalog)
	if err == nil {
		t.Fatal("an unknown top-level field must be rejected")
	}
	if !strings.Contains(err.Error(), "extra") {
		t.Fatalf("error %q should name the offending field", err.Error())
	}
}
