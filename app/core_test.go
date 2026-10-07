package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/app"
	"github.com/gosomea/deepseek-harness-go/cordis"
	"github.com/gosomea/deepseek-harness-go/loader"
)

func simpleCatalog(t *testing.T, names ...string) *loader.Catalog {
	t.Helper()
	catalog := loader.NewCatalog()
	for _, name := range names {
		if err := catalog.Register(name, func() cordis.Plugin {
			return cordis.Plugin{
				Name:  name,
				Apply: func(*cordis.Context, any) (cordis.Cleanup, error) { return nil, nil },
			}
		}); err != nil {
			t.Fatal(err)
		}
	}
	return catalog
}

// TestResolveAppliesBundleOrderThenProfileLayer pins the documented precedence:
// bundle layers in the profile's order, then the profile's own layer last.
func TestResolveAppliesBundleOrderThenProfileLayer(t *testing.T) {
	bundles := app.NewCatalog()
	for _, name := range []string{"base", "extra"} {
		if err := bundles.Register(app.Bundle{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	profile, err := app.ParseProfile([]byte(`{"name":"dev","bundles":["base","extra"]}`))
	if err != nil {
		t.Fatal(err)
	}
	layers, err := app.Resolve(profile, bundles)
	if err != nil {
		t.Fatal(err)
	}
	if len(layers) != 2 {
		t.Fatalf("layers = %d, want 2", len(layers))
	}
	if layers[0].Source != "bundle/base" || layers[1].Source != "bundle/extra" {
		t.Fatalf("layer order = %q, %q", layers[0].Source, layers[1].Source)
	}

	// A profile-owned patch layer is appended last, so it overrides bundles.
	profile, err = app.ParseProfile([]byte(`{"name":"dev","bundles":["base"],"patches":[{"id":"x","disabled":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	layers, err = app.Resolve(profile, bundles)
	if err != nil {
		t.Fatal(err)
	}
	if len(layers) != 2 || layers[1].Source != "profile/dev" {
		t.Fatalf("layers = %+v", layers)
	}
}

// TestResolveRejectsUnknownBundleBeforeComposing pins that a missing layer is an
// error rather than a silently thinner tree.
func TestResolveRejectsUnknownBundleBeforeComposing(t *testing.T) {
	bundles := app.NewCatalog()
	if err := bundles.Register(app.Bundle{Name: "base"}); err != nil {
		t.Fatal(err)
	}
	profile, err := app.ParseProfile([]byte(`{"name":"dev","bundles":["base","ghost"]}`))
	if err != nil {
		t.Fatal(err)
	}
	layers, err := app.Resolve(profile, bundles)
	if !errors.Is(err, app.ErrUnknownBundle) {
		t.Fatalf("Resolve = %v", err)
	}
	if layers != nil {
		t.Fatalf("a rejected profile must not produce layers: %+v", layers)
	}
}

// TestCatalogRegistrationRules keeps duplicate and empty bundle names observable.
func TestCatalogRegistrationRules(t *testing.T) {
	catalog := app.NewCatalog()
	if err := catalog.Register(app.Bundle{Name: ""}); !errors.Is(err, app.ErrInvalidBundle) {
		t.Fatalf("empty name: %v", err)
	}
	if err := catalog.Register(app.Bundle{Name: "base"}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.Register(app.Bundle{Name: "base"}); !errors.Is(err, app.ErrInvalidBundle) {
		t.Fatalf("duplicate name: %v", err)
	}
	if _, err := catalog.Lookup("ghost"); !errors.Is(err, app.ErrUnknownBundle) {
		t.Fatalf("unknown lookup: %v", err)
	}
	if got := catalog.Names(); len(got) != 1 || got[0] != "base" {
		t.Fatalf("names = %v", got)
	}
}

// TestParseProfileRejectsInvalidDeclarations keeps profile typos loud.
func TestParseProfileRejectsInvalidDeclarations(t *testing.T) {
	for _, declaration := range []string{
		`{"bundles":[]}`,
		`{"name":"x","bundles":[""]}`,
		`{"name":"x","required":[""]}`,
		`{"name":"x","nope":1}`,
		`not json`,
	} {
		if _, err := app.ParseProfile([]byte(declaration)); !errors.Is(err, app.ErrInvalidProfile) {
			t.Fatalf("declaration %s: %v", declaration, err)
		}
	}
}

// TestParseRequirementForms pins the two accepted required-entry forms.
func TestParseRequirementForms(t *testing.T) {
	for _, tc := range []struct {
		value string
		id    string
		name  string
	}{
		{"greeter", "greeter", ""},
		{"gate:inner", "gate", "inner"},
	} {
		got, err := app.ParseRequirement(tc.value)
		if err != nil {
			t.Fatalf("%s: %v", tc.value, err)
		}
		if got.ID != tc.id || got.Name != tc.name {
			t.Fatalf("%s = {id:%q name:%q}, want {id:%q name:%q}", tc.value, got.ID, got.Name, tc.id, tc.name)
		}
	}
	if _, err := app.ParseRequirement(""); !errors.Is(err, app.ErrInvalidProfile) {
		t.Fatalf("empty requirement: %v", err)
	}
}

// TestRequiredEntryBlocksReadinessWhileSiblingsOnlyReport pins the core required
// rule: a required entry that cannot run fails the profile, while a failing
// sibling is reported without making the profile unusable.
func TestRequiredEntryBlocksReadinessWhileSiblingsOnlyReport(t *testing.T) {
	plugins := loader.NewCatalog()
	if err := plugins.Register("ok", func() cordis.Plugin {
		return cordis.Plugin{Name: "ok", Apply: func(*cordis.Context, any) (cordis.Cleanup, error) { return nil, nil }}
	}); err != nil {
		t.Fatal(err)
	}
	if err := plugins.Register("boom", func() cordis.Plugin {
		return cordis.Plugin{Name: "boom", Apply: func(*cordis.Context, any) (cordis.Cleanup, error) {
			return nil, errors.New("apply failed")
		}}
	}); err != nil {
		t.Fatal(err)
	}

	bundles := app.NewCatalog()
	if err := bundles.Register(app.Bundle{Name: "base", Patches: []app.Patch{
		{ID: "", Insert: []loader.Options{{ID: "good", Name: "ok"}, {ID: "broken", Name: "boom"}}},
	}}); err != nil {
		t.Fatal(err)
	}

	// The failing entry is not required: it is reported, and the profile is usable.
	profile, err := app.ParseProfile([]byte(`{"name":"dev","bundles":["base"],"required":["good"]}`))
	if err != nil {
		t.Fatal(err)
	}
	ready, err := app.Mount(context.Background(), profile, bundles, plugins)
	if err != nil {
		t.Fatalf("a non-required failure must not block readiness: %v", err)
	}
	defer func() { _ = ready.Close(context.Background()) }()
	if len(ready.Active) != 1 || ready.Active[0] != "good" {
		t.Fatalf("active = %v", ready.Active)
	}
	// The sibling is reported without blocking readiness: it appears in Siblings,
	// not in Failed, so a caller can tell a partial profile from a fatal one.
	if len(ready.Failed) != 0 {
		t.Fatalf("a non-required failure must not appear in Failed: %v", ready.Failed)
	}
	if len(ready.Siblings) != 1 || ready.Siblings[0].ID != "broken" {
		t.Fatalf("siblings = %+v", ready.Siblings)
	}
	if !strings.Contains(ready.Siblings[0].Reason.Error(), "apply failed") {
		t.Fatalf("sibling reason = %v", ready.Siblings[0].Reason)
	}

	// Making the failing entry required blocks readiness.
	profile, err = app.ParseProfile([]byte(`{"name":"dev","bundles":["base"],"required":["good","broken"]}`))
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := app.Mount(context.Background(), profile, bundles, plugins)
	if !errors.Is(err, app.ErrRequiredEntryFailed) {
		t.Fatalf("a required failure must block readiness: %v", err)
	}
	if len(blocked.Failed) != 1 || blocked.Failed[0].ID != "broken" {
		t.Fatalf("failed = %+v", blocked.Failed)
	}
	if err := blocked.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestRequiredEntryMissingAndPendingBlockReadiness covers the other two ways a
// required entry fails: it was never composed, or it waits on a dependency.
func TestRequiredEntryMissingAndPendingBlockReadiness(t *testing.T) {
	plugins := loader.NewCatalog()
	if err := plugins.Register("needs", func() cordis.Plugin {
		return cordis.Plugin{
			Name:   "needs",
			Inject: []string{"absent-service"},
			Apply:  func(*cordis.Context, any) (cordis.Cleanup, error) { return nil, nil },
		}
	}); err != nil {
		t.Fatal(err)
	}
	bundles := app.NewCatalog()
	if err := bundles.Register(app.Bundle{Name: "base", Patches: []app.Patch{
		{ID: "", Insert: []loader.Options{{ID: "waits", Name: "needs"}}},
	}}); err != nil {
		t.Fatal(err)
	}

	profile, err := app.ParseProfile([]byte(`{"name":"dev","bundles":["base"],"required":["waits","never-composed"]}`))
	if err != nil {
		t.Fatal(err)
	}
	ready, err := app.Mount(context.Background(), profile, bundles, plugins)
	if !errors.Is(err, app.ErrRequiredEntryFailed) {
		t.Fatalf("Mount = %v", err)
	}
	defer func() { _ = ready.Close(context.Background()) }()
	if len(ready.Missing) != 1 || ready.Missing[0] != "never-composed" {
		t.Fatalf("missing = %v", ready.Missing)
	}
	foundPending := false
	for _, failure := range ready.Failed {
		if failure.ID == "waits" {
			foundPending = true
		}
	}
	if !foundPending {
		t.Fatalf("a pending required entry must fail readiness: %+v", ready.Failed)
	}
	if len(ready.Pending) != 1 || ready.Pending[0] != "waits" {
		t.Fatalf("pending = %v", ready.Pending)
	}
	if len(ready.Siblings) != 0 {
		t.Fatalf("every composed entry here is required, so no sibling should be reported: %+v", ready.Siblings)
	}
}

// TestRequiredNameAssertionRejectsRename keeps "id:name" from silently accepting
// a renamed plugin.
func TestRequiredNameAssertionRejectsRename(t *testing.T) {
	plugins := simpleCatalog(t, "actual")
	bundles := app.NewCatalog()
	if err := bundles.Register(app.Bundle{Name: "base", Patches: []app.Patch{
		{ID: "", Insert: []loader.Options{{ID: "entry", Name: "actual"}}},
	}}); err != nil {
		t.Fatal(err)
	}
	profile, err := app.ParseProfile([]byte(`{"name":"dev","bundles":["base"],"required":["entry:expected"]}`))
	if err != nil {
		t.Fatal(err)
	}
	ready, err := app.Mount(context.Background(), profile, bundles, plugins)
	if !errors.Is(err, app.ErrRequiredEntryFailed) {
		t.Fatalf("Mount = %v", err)
	}
	defer func() { _ = ready.Close(context.Background()) }()
	if len(ready.Failed) != 1 || !strings.Contains(ready.Failed[0].Reason.Error(), "expected") {
		t.Fatalf("failed = %+v", ready.Failed)
	}
}

// TestDuplicateRequirementIsRejected keeps one profile from stating two
// expectations for the same entry.
func TestDuplicateRequirementIsRejected(t *testing.T) {
	plugins := simpleCatalog(t, "ok")
	bundles := app.NewCatalog()
	profile, err := app.ParseProfile([]byte(`{"name":"dev","required":["a","a"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Mount(context.Background(), profile, bundles, plugins); !errors.Is(err, app.ErrInvalidProfile) {
		t.Fatalf("duplicate requirement: %v", err)
	}
}

// TestMountReportsSkippedOverlays keeps a typo in an overlay visible through the
// readiness result, and lets a caller decide whether it is fatal.
func TestMountReportsSkippedOverlays(t *testing.T) {
	plugins := simpleCatalog(t, "ok")
	bundles := app.NewCatalog()
	if err := bundles.Register(app.Bundle{Name: "base", Patches: []app.Patch{
		{ID: "", Insert: []loader.Options{{ID: "good", Name: "ok"}}},
		{ID: "typo", Config: nil},
	}}); err != nil {
		t.Fatal(err)
	}
	profile, err := app.ParseProfile([]byte(`{"name":"dev","bundles":["base"],"required":["good"]}`))
	if err != nil {
		t.Fatal(err)
	}
	ready, err := app.Mount(context.Background(), profile, bundles, plugins)
	if err != nil {
		t.Fatalf("a skipped overlay must not block readiness by itself: %v", err)
	}
	defer func() { _ = ready.Close(context.Background()) }()
	if len(ready.Composition.Skipped) != 1 || ready.Composition.Skipped[0].ID != "typo" {
		t.Fatalf("skipped = %+v", ready.Composition.Skipped)
	}
	if !errors.Is(ready.Composition.Err(), app.ErrPatchSkipped) {
		t.Fatalf("composition error = %v", ready.Composition.Err())
	}
}

// TestMountRejectsUnknownPluginBeforeMounting pins that a bad entry leaves
// nothing running and no resource behind.
func TestMountRejectsUnknownPluginBeforeMounting(t *testing.T) {
	plugins := simpleCatalog(t, "ok")
	bundles := app.NewCatalog()
	if err := bundles.Register(app.Bundle{Name: "base", Patches: []app.Patch{
		{ID: "", Insert: []loader.Options{{ID: "a", Name: "ok"}, {ID: "b", Name: "missing"}}},
	}}); err != nil {
		t.Fatal(err)
	}
	profile, err := app.ParseProfile([]byte(`{"name":"dev","bundles":["base"]}`))
	if err != nil {
		t.Fatal(err)
	}
	ready, err := app.Mount(context.Background(), profile, bundles, plugins)
	if !errors.Is(err, loader.ErrUnknownPlugin) {
		t.Fatalf("Mount = %v", err)
	}
	if ready.Tree != nil {
		t.Fatal("a rejected composition must not leave a running tree")
	}
}

// TestNilInputsAreRejected keeps programmatic misuse observable.
func TestNilInputsAreRejected(t *testing.T) {
	if _, err := app.Resolve(nil, app.NewCatalog()); !errors.Is(err, app.ErrInvalidProfile) {
		t.Fatalf("nil profile: %v", err)
	}
	profile, err := app.ParseProfile([]byte(`{"name":"dev"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Resolve(profile, nil); !errors.Is(err, app.ErrInvalidProfile) {
		t.Fatalf("nil catalog: %v", err)
	}
	if _, err := app.Mount(context.Background(), profile, app.NewCatalog(), nil); !errors.Is(err, app.ErrInvalidProfile) {
		t.Fatalf("nil plugin catalog: %v", err)
	}
}

// TestComposeIsDeterministicAndNonMutating pins that composing twice from the
// same inputs yields the same tree and never mutates its input.
func TestComposeIsDeterministicAndNonMutating(t *testing.T) {
	shared := []loader.Options{{ID: "a", Name: "one"}}
	config := json.RawMessage(`{"n":1}`)
	layers := []app.Layer{{Source: "l1", Patches: []app.Patch{{ID: "a", Config: &config}}}}
	first := app.Compose(append([]app.Layer{{Source: "base", Patches: []app.Patch{{ID: "", Insert: shared}}}}, layers...))
	second := app.Compose(append([]app.Layer{{Source: "base", Patches: []app.Patch{{ID: "", Insert: shared}}}}, layers...))
	if len(first.Entries) != len(second.Entries) {
		t.Fatalf("entry counts differ: %d vs %d", len(first.Entries), len(second.Entries))
	}
	if string(first.Entries[0].Config) != string(second.Entries[0].Config) {
		t.Fatalf("configs differ: %s vs %s", first.Entries[0].Config, second.Entries[0].Config)
	}
	// The input list must be untouched by the patch layer.
	if len(shared[0].Config) != 0 {
		t.Fatalf("compose mutated its input: %s", shared[0].Config)
	}
}

// TestSkippedDiagnosticsSortStable keeps reporting order deterministic.
func TestSkippedDiagnosticsSortStable(t *testing.T) {
	result := app.Compose([]app.Layer{{Source: "l", Patches: []app.Patch{
		{ID: "z"}, {ID: "a"}, {ID: "m"},
	}}})
	if len(result.Skipped) != 3 {
		t.Fatalf("skipped = %+v", result.Skipped)
	}
	ordered := app.SortSkipped(result.Skipped)
	if ordered[0].ID != "a" || ordered[1].ID != "m" || ordered[2].ID != "z" {
		t.Fatalf("sorted = %+v", ordered)
	}
}

// TestRegistrationOnlyClaimIsDocumented pins the non-compatibility statement to
// the package documentation so it cannot be dropped silently.
func TestRegistrationOnlyClaimIsDocumented(t *testing.T) {
	body, err := os.ReadFile("doc.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "explicit compile-time registration only") {
		t.Fatal("doc.go must state that bundles are compile-time registrations only")
	}
}
