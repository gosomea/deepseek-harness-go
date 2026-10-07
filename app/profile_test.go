package app_test

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/gosomea/deepseek-harness-go/app"
	"github.com/gosomea/deepseek-harness-go/loader"
)

const fixturePath = "../testdata/parity/loader/profile-overlay.json"

// overlayFixture is the recorded overlay spec: three layers over one empty list.
type overlayFixture struct {
	ID        string   `json:"id"`
	Semantics []string `json:"semantics"`
	Layers    []struct {
		Source  string      `json:"source"`
		Patches []app.Patch `json:"patches"`
	} `json:"layers"`
	Required []string `json:"required"`
	Expected struct {
		Entries []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Group bool   `json:"group"`
		} `json:"entries"`
		GreeterConfig json.RawMessage `json:"greeter_config"`
		GreeterInject []string        `json:"greeter_inject"`
		InnerConfig   json.RawMessage `json:"inner_config"`
		Skipped       []string        `json:"skipped"`
	} `json:"expected"`
}

func loadFixture(t *testing.T) overlayFixture {
	t.Helper()
	body, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	var fixture overlayFixture
	if err := json.Unmarshal(body, &fixture); err != nil {
		t.Fatalf("parsing fixture: %v", err)
	}
	return fixture
}

// TestFixtureRecordsOverlaySemantics pins the recorded spec itself, so the
// fixture cannot silently lose the rules it exists to freeze.
func TestFixtureRecordsOverlaySemantics(t *testing.T) {
	fixture := loadFixture(t)
	if fixture.ID != "profile-overlay" {
		t.Fatalf("fixture id = %q", fixture.ID)
	}
	if len(fixture.Layers) != 3 {
		t.Fatalf("layers = %d, want 3", len(fixture.Layers))
	}
	if len(fixture.Semantics) == 0 {
		t.Fatal("the fixture must state the semantics it fixes")
	}
	for _, required := range []string{"greeter", "gate"} {
		found := false
		for _, want := range fixture.Required {
			if want == required {
				found = true
			}
		}
		if !found {
			t.Fatalf("required %q missing from fixture", required)
		}
	}
}

// TestOverlayLayersApplyInOrderWithLaterOverride is the fixture-driven proof of
// replacement semantics: each layer wins over the previous one on the same id.
func TestOverlayLayersApplyInOrderWithLaterOverride(t *testing.T) {
	fixture := loadFixture(t)
	layers := make([]app.Layer, 0, len(fixture.Layers))
	for _, layer := range fixture.Layers {
		layers = append(layers, app.Layer{Source: layer.Source, Patches: layer.Patches})
	}

	result := app.Compose(layers)

	// Entry set and order match the fixture's expectation.
	if len(result.Entries) != len(fixture.Expected.Entries) {
		t.Fatalf("entries = %d, want %d", len(result.Entries), len(fixture.Expected.Entries))
	}
	for index, want := range fixture.Expected.Entries {
		got := result.Entries[index]
		if got.ID != want.ID || got.Name != want.Name || got.IsGroup() != want.Group {
			t.Fatalf("entries[%d] = {id:%q name:%q group:%v}, want {id:%q name:%q group:%v}",
				index, got.ID, got.Name, got.IsGroup(), want.ID, want.Name, want.Group)
		}
	}

	// The last layer to touch each id wins: level 2 from bundle/extra, not level 1.
	greeter := entryAt(t, result.Entries, "greeter")
	assertJSONEqual(t, greeter.Config, fixture.Expected.GreeterConfig, "greeter config")
	if len(greeter.Inject) != len(fixture.Expected.GreeterInject) || greeter.Inject[0] != fixture.Expected.GreeterInject[0] {
		t.Fatalf("greeter inject = %v, want %v", greeter.Inject, fixture.Expected.GreeterInject)
	}

	// A nested entry is patched by its own id across layers.
	gate := entryAt(t, result.Entries, "gate")
	children, err := json.Marshal(gate.Config)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(children, &decoded); err != nil {
		t.Fatalf("gate children: %v", err)
	}
	inner, ok := decoded[0]["config"].(map[string]any)
	if !ok {
		t.Fatalf("inner config = %#v", decoded[0]["config"])
	}
	if got := int(inner["size"].(float64)); got != 9 {
		t.Fatalf("inner size = %d, want 9 (profile layer wins)", got)
	}

	// The profile layer's insert added its entry at the root.
	if _, ok := findEntry(result.Entries, "added"); !ok {
		t.Fatal("the insert patch should have added entry \"added\"")
	}

	// The unmatched patch is reported, not silently ignored.
	if len(result.Skipped) != len(fixture.Expected.Skipped) {
		t.Fatalf("skipped = %v, want %v", result.Skipped, fixture.Expected.Skipped)
	}
	if result.Skipped[0].ID != fixture.Expected.Skipped[0] {
		t.Fatalf("skipped[0] = %q, want %q", result.Skipped[0].ID, fixture.Expected.Skipped[0])
	}
	if err := result.Err(); !errors.Is(err, app.ErrPatchSkipped) {
		t.Fatalf("ComposeResult.Err() = %v, want ErrPatchSkipped", err)
	}
}

func entryAt(t *testing.T, entries []loader.Options, id string) loader.Options {
	t.Helper()
	entry, ok := findEntry(entries, id)
	if !ok {
		t.Fatalf("entry %q not found", id)
	}
	return *entry
}

func findEntry(entries []loader.Options, id string) (*loader.Options, bool) {
	for index := range entries {
		if entries[index].ID == id {
			return &entries[index], true
		}
	}
	return nil, false
}

// assertJSONEqual compares two JSON payloads by value, so re-encoding by a patch
// layer is not mistaken for a semantic difference.
func assertJSONEqual(t *testing.T, got, want []byte, label string) {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("%s: parsing got %s: %v", label, got, err)
	}
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("%s: parsing want %s: %v", label, want, err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("%s = %s, want %s", label, got, want)
	}
}
