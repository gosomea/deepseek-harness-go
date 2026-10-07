package loader_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/cordis"
	"github.com/gosomea/deepseek-harness-go/loader"
)

// recordingPlugin returns a factory whose plugin records the config it validated.
func recordingPlugin(name string, seen *[]any) loader.Factory {
	return func() cordis.Plugin {
		return cordis.Plugin{
			Name: name,
			Apply: func(*cordis.Context, any) (cordis.Cleanup, error) {
				return nil, nil
			},
			Validate: func(config any) (any, error) {
				if seen != nil {
					*seen = append(*seen, config)
				}
				return config, nil
			},
		}
	}
}

func mustCatalog(t *testing.T, names ...string) *loader.Catalog {
	t.Helper()
	catalog := loader.NewCatalog()
	for _, name := range names {
		if err := catalog.Register(name, recordingPlugin(name, nil)); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}
	return catalog
}

// TestLoadResolvesEntriesWithoutSideEffects pins the success path: a document
// resolves to entries that expose the document's identity, the catalog plugin and
// the plugin-validated configuration, and nothing is applied or registered.
func TestLoadResolvesEntriesWithoutSideEffects(t *testing.T) {
	var seen []any
	catalog := loader.NewCatalog()
	if err := catalog.Register("alpha", recordingPlugin("alpha", &seen)); err != nil {
		t.Fatal(err)
	}
	document := `{
	  "entries": [
	    {"id": "one", "name": "alpha", "config": {"level": 3}, "inject": "store"},
	    {"id": "two", "name": "alpha", "config": [1, 2], "inject": ["store", "clock"]}
	  ]
	}`

	entries, err := loader.Load([]byte(document), catalog)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	for index, wantID := range []string{"one", "two"} {
		if entries[index].ID() != wantID {
			t.Fatalf("entries[%d].ID() = %q, want %q", index, entries[index].ID(), wantID)
		}
		if entries[index].Name() != "alpha" {
			t.Fatalf("entries[%d].Name() = %q", index, entries[index].Name())
		}
		if !entries[index].Runnable() {
			t.Fatalf("entries[%d] should be runnable", index)
		}
		if entries[index].Plugin().Name != "alpha" {
			t.Fatalf("entries[%d] plugin = %q", index, entries[index].Plugin().Name)
		}
	}
	if len(seen) != 2 {
		t.Fatalf("plugin Validate calls = %d, want 2 (the plugin owns validation)", len(seen))
	}
	// The codec must not rewrite the document: the raw payload is readable back.
	if got := string(entries[0].Options().Config); got != `{"level": 3}` {
		t.Fatalf("raw config = %s", got)
	}
	if got := entries[1].Options().Inject; len(got) != 2 || got[0] != "store" || got[1] != "clock" {
		t.Fatalf("inject = %v", got)
	}
	if got := entries[0].Options().Inject; len(got) != 1 || got[0] != "store" {
		t.Fatalf("single inject string should decode as one name, got %v", got)
	}
}

// TestEntryOptionsAreCopies protects the entry from callers mutating what they read.
func TestEntryOptionsAreCopies(t *testing.T) {
	catalog := mustCatalog(t, "alpha")
	entries, err := loader.Load([]byte(`{"entries":[{"id":"one","name":"alpha","config":{"a":1},"inject":["store"]}]}`), catalog)
	if err != nil {
		t.Fatal(err)
	}
	options := entries[0].Options()
	options.Inject[0] = "mutated"
	options.Config[0] = 'X'
	if got := entries[0].Options().Inject[0]; got != "store" {
		t.Fatalf("mutating the returned options changed the entry: %q", got)
	}
	if got := string(entries[0].Options().Config); got != `{"a":1}` {
		t.Fatalf("mutating the returned config changed the entry: %s", got)
	}
}

// TestDisabledAndGroupEntriesResolveWithoutPlugin pins that a disabled entry and a
// group entry are valid entries with distinguishable reasons for not running.
func TestDisabledAndGroupEntriesResolveWithoutPlugin(t *testing.T) {
	catalog := mustCatalog(t, "alpha")
	document := `{
	  "entries": [
	    {"id": "off", "name": "alpha", "disabled": true},
	    {"id": "grp", "group": true}
	  ]
	}`
	entries, err := loader.Load([]byte(document), catalog)
	if err != nil {
		t.Fatalf("disabled and group entries must still parse: %v", err)
	}
	if entries[0].Runnable() || !entries[0].Disabled() || entries[0].IsGroup() {
		t.Fatalf("disabled entry: runnable=%v disabled=%v group=%v", entries[0].Runnable(), entries[0].Disabled(), entries[0].IsGroup())
	}
	if entries[1].Runnable() || entries[1].Disabled() || !entries[1].IsGroup() {
		t.Fatalf("group entry: runnable=%v disabled=%v group=%v", entries[1].Runnable(), entries[1].Disabled(), entries[1].IsGroup())
	}
	if entries[0].Plugin().Name != "" || entries[1].Config() != nil {
		t.Fatal("an entry that does not run must not carry a plugin or validated config")
	}
}

// TestFailureClassesStayDistinct is the rejection half of the contract: an unknown
// plugin name, a duplicate id, an unknown field and a wrong value type must each
// be observable and must not collapse into one generic error.
func TestFailureClassesStayDistinct(t *testing.T) {
	catalog := mustCatalog(t, "alpha")
	good := `{"entries":[{"id":"one","name":"alpha"}]}`
	if _, err := loader.Load([]byte(good), catalog); err != nil {
		t.Fatalf("control document should load: %v", err)
	}

	for _, tc := range []struct {
		name     string
		doc      string
		sentinel error
		detail   string
	}{
		{
			name:     "unknown plugin name",
			doc:      `{"entries":[{"id":"one","name":"missing"}]}`,
			sentinel: loader.ErrUnknownPlugin,
			detail:   "missing",
		},
		{
			name:     "duplicate id",
			doc:      `{"entries":[{"id":"dup","name":"alpha"},{"id":"dup","name":"alpha"}]}`,
			sentinel: loader.ErrDuplicateID,
			detail:   "dup",
		},
		{
			name:     "unknown field",
			doc:      `{"entries":[{"id":"one","name":"alpha","nope":1}]}`,
			sentinel: loader.ErrInvalidConfig,
			detail:   "unknown field",
		},
		{
			name:     "wrong value type",
			doc:      `{"entries":[{"id":"one","name":"alpha","inject":42}]}`,
			sentinel: loader.ErrInvalidConfig,
			detail:   "inject",
		},
		{
			name:     "empty document",
			doc:      `{"entries":[]}`,
			sentinel: loader.ErrInvalidConfig,
			detail:   "no entries",
		},
		{
			name:     "missing id",
			doc:      `{"entries":[{"name":"alpha"}]}`,
			sentinel: loader.ErrInvalidConfig,
			detail:   "no id",
		},
		{
			name:     "trailing content",
			doc:      `{"entries":[{"id":"one","name":"alpha"}]}{}`,
			sentinel: loader.ErrInvalidConfig,
			detail:   "trailing content",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entries, err := loader.Load([]byte(tc.doc), catalog)
			if err == nil {
				t.Fatalf("expected rejection, got %d entries", len(entries))
			}
			if !errors.Is(err, tc.sentinel) {
				t.Fatalf("error %v does not wrap %v", err, tc.sentinel)
			}
			if !strings.Contains(err.Error(), tc.detail) {
				t.Fatalf("error %q should mention %q", err.Error(), tc.detail)
			}
		})
	}
}

// TestUnknownNameDoesNotCollapseIntoInvalidConfig keeps the two structural
// failures apart: a host must be able to tell "you named something I do not have"
// from "your document is malformed".
func TestUnknownNameDoesNotCollapseIntoInvalidConfig(t *testing.T) {
	catalog := mustCatalog(t, "alpha")
	_, unknownErr := loader.Load([]byte(`{"entries":[{"id":"one","name":"missing"}]}`), catalog)
	if errors.Is(unknownErr, loader.ErrInvalidConfig) {
		t.Fatal("an unknown plugin name must not also report a malformed document")
	}
	_, invalidErr := loader.Load([]byte(`{"entries":[{"id":"one","name":"alpha","nope":1}]}`), catalog)
	if errors.Is(invalidErr, loader.ErrUnknownPlugin) {
		t.Fatal("a malformed document must not report an unknown plugin name")
	}
}

// TestResolveRejectsNilInputs keeps programmatic misuse observable.
func TestResolveRejectsNilInputs(t *testing.T) {
	if _, err := loader.Resolve(nil, loader.NewCatalog()); !errors.Is(err, loader.ErrInvalidConfig) {
		t.Fatalf("nil document: %v", err)
	}
	document, err := loader.ParseDocument([]byte(`{"entries":[{"id":"one","name":"alpha"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loader.Resolve(document, nil); !errors.Is(err, loader.ErrInvalidConfig) {
		t.Fatalf("nil catalog: %v", err)
	}
}

// assertRawJSONShape keeps the Options field names pinned to the document format,
// so a rename cannot silently change the file format the codec accepts.
func TestOptionsJSONFieldNames(t *testing.T) {
	var options loader.Options
	body, err := json.Marshal(loader.Options{ID: "one", Name: "alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &options); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"id": true, "name": true}
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	for key := range decoded {
		if !want[key] {
			t.Fatalf("unexpected field %q in encoded options", key)
		}
	}
	if options.ID != "one" || options.Name != "alpha" {
		t.Fatalf("round trip lost identity: %+v", options)
	}
}
