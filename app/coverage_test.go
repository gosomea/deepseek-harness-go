package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/gosomea/deepseek-harness-go/app"
	"github.com/gosomea/deepseek-harness-go/cordis"
	"github.com/gosomea/deepseek-harness-go/loader"
)

// TestInsertIntoGroupTarget covers the insert variant that appends to a named group.
func TestInsertIntoGroupTarget(t *testing.T) {
	result := app.Compose([]app.Layer{{Source: "l", Patches: []app.Patch{
		{ID: "", Insert: []loader.Options{{ID: "g", Group: boolPtr(true), Config: json.RawMessage(`[]`)}}},
		{ID: "g", Insert: []loader.Options{{ID: "kid", Name: "worker"}}},
	}}})
	if len(result.Skipped) != 0 {
		t.Fatalf("skipped = %+v", result.Skipped)
	}
	if len(result.Entries) != 1 {
		t.Fatalf("entries = %d", len(result.Entries))
	}
	// The appended child is readable back from the group's encoded payload.
	var children []loader.Options
	if err := json.Unmarshal(result.Entries[0].Config, &children); err != nil {
		t.Fatalf("group payload: %v", err)
	}
	if len(children) != 1 || children[0].ID != "kid" {
		t.Fatalf("children = %+v", children)
	}
}

// TestInsertIntoNonGroupIsSkipped keeps a misplaced insert observable.
func TestInsertIntoNonGroupIsSkipped(t *testing.T) {
	result := app.Compose([]app.Layer{{Source: "l", Patches: []app.Patch{
		{ID: "", Insert: []loader.Options{{ID: "plain", Name: "worker"}}},
		{ID: "plain", Insert: []loader.Options{{ID: "kid", Name: "worker"}}},
	}}})
	if len(result.Skipped) != 1 || !strings.Contains(result.Skipped[0].Reason, "not a group") {
		t.Fatalf("skipped = %+v", result.Skipped)
	}
}

// TestInsertIntoMissingGroupIsSkipped covers an insert whose target does not exist.
func TestInsertIntoMissingGroupIsSkipped(t *testing.T) {
	result := app.Compose([]app.Layer{{Source: "l", Patches: []app.Patch{
		{ID: "ghost", Insert: []loader.Options{{ID: "kid", Name: "worker"}}},
	}}})
	if len(result.Skipped) != 1 || !strings.Contains(result.Skipped[0].Reason, "not a group") {
		t.Fatalf("skipped = %+v", result.Skipped)
	}
}

// TestPatchRequiresID covers a non-insert patch with no id.
func TestPatchRequiresID(t *testing.T) {
	config := json.RawMessage(`1`)
	result := app.Compose([]app.Layer{{Source: "l", Patches: []app.Patch{{Config: &config}}}})
	if len(result.Skipped) != 1 || !strings.Contains(result.Skipped[0].Reason, "id is required") {
		t.Fatalf("skipped = %+v", result.Skipped)
	}
}

// TestPatchNameMismatchIsSkipped covers the name guard on a non-insert patch.
func TestPatchNameMismatchIsSkipped(t *testing.T) {
	mismatchConfig := json.RawMessage(`{"x":1}`)
	result := app.Compose([]app.Layer{{Source: "l", Patches: []app.Patch{
		{ID: "", Insert: []loader.Options{{ID: "a", Name: "actual"}}},
		{ID: "a", Name: stringPtr("other"), Config: &mismatchConfig},
	}}})
	if len(result.Skipped) != 1 || !strings.Contains(result.Skipped[0].Reason, "name mismatch") {
		t.Fatalf("skipped = %+v", result.Skipped)
	}
	// A skipped patch must not have changed the entry.
	if len(result.Entries[0].Config) != 0 {
		t.Fatalf("a skipped patch must not apply: %s", result.Entries[0].Config)
	}
}

// TestPatchDisabledAndInjectOverride covers the remaining patch fields.
func TestPatchDisabledAndInjectOverride(t *testing.T) {
	inject := loader.Inject{"clock"}
	disabled := true
	result := app.Compose([]app.Layer{{Source: "l", Patches: []app.Patch{
		{ID: "", Insert: []loader.Options{{ID: "a", Name: "worker"}}},
		{ID: "a", Inject: &inject, Disabled: &disabled},
	}}})
	if len(result.Skipped) != 0 {
		t.Fatalf("skipped = %+v", result.Skipped)
	}
	entry := result.Entries[0]
	if !entry.IsDisabled() || len(entry.Inject) != 1 || entry.Inject[0] != "clock" {
		t.Fatalf("entry = %+v", entry)
	}
}

// TestMalformedGroupPayloadIsReported keeps an invalid group payload from being
// silently dropped during composition.
func TestMalformedGroupPayloadIsReported(t *testing.T) {
	result := app.Compose([]app.Layer{{Source: "l", Patches: []app.Patch{
		{ID: "", Insert: []loader.Options{{ID: "g", Group: boolPtr(true), Config: json.RawMessage(`"not-a-list"`)}}},
	}}})
	if len(result.Skipped) != 1 {
		t.Fatalf("skipped = %+v", result.Skipped)
	}
	if !errors.Is(result.Err(), app.ErrPatchSkipped) {
		t.Fatalf("Err = %v", result.Err())
	}
}

// TestGroupPayloadIsRecomposedAfterNestedPatch pins that a nested patch reaches
// the encoded result rather than a discarded copy.
func TestGroupPayloadIsRecomposedAfterNestedPatch(t *testing.T) {
	config := json.RawMessage(`{"size":9}`)
	result := app.Compose([]app.Layer{{Source: "l", Patches: []app.Patch{
		{ID: "", Insert: []loader.Options{{ID: "g", Group: boolPtr(true), Config: json.RawMessage(`[{"id":"kid","name":"worker","config":{"size":1}}]`)}}},
		{ID: "kid", Config: &config},
	}}})
	if len(result.Skipped) != 0 {
		t.Fatalf("skipped = %+v", result.Skipped)
	}
	var children []loader.Options
	if err := json.Unmarshal(result.Entries[0].Config, &children); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(children[0].Config), "9") {
		t.Fatalf("nested patch did not reach the result: %s", children[0].Config)
	}
}

// TestNestedPatchByFullPath covers resolving a nested entry by its full path.
func TestNestedPatchByFullPath(t *testing.T) {
	config := json.RawMessage(`{"size":7}`)
	result := app.Compose([]app.Layer{{Source: "l", Patches: []app.Patch{
		{ID: "", Insert: []loader.Options{{ID: "g", Group: boolPtr(true), Config: json.RawMessage(`[{"id":"kid","name":"worker"}]`)}}},
		{ID: "g:kid", Config: &config},
	}}})
	if len(result.Skipped) != 0 {
		t.Fatalf("skipped = %+v", result.Skipped)
	}
	var children []loader.Options
	if err := json.Unmarshal(result.Entries[0].Config, &children); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(children[0].Config), "7") {
		t.Fatalf("full-path patch did not apply: %s", children[0].Config)
	}
}

// TestComposeProfileReportsUnknownBundleBeforeComposing keeps a missing layer fatal.
func TestComposeProfileReportsUnknownBundleBeforeComposing(t *testing.T) {
	catalog := app.NewCatalog()
	profile, err := app.ParseProfile([]byte(`{"name":"dev","bundles":["ghost"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.ComposeProfile(profile, catalog); !errors.Is(err, app.ErrUnknownBundle) {
		t.Fatalf("ComposeProfile = %v", err)
	}
}

// TestReadyCloseIsIdempotentAndTreeOwned covers the profile close path.
func TestReadyCloseIsIdempotentAndTreeOwned(t *testing.T) {
	if err := (app.Ready{}).Close(context.Background()); err != nil {
		t.Fatalf("closing a profile that never mounted: %v", err)
	}
	plugins := simpleCatalog(t, "ok")
	bundles := app.NewCatalog()
	if err := bundles.Register(app.Bundle{Name: "base", Patches: []app.Patch{
		{ID: "", Insert: []loader.Options{{ID: "a", Name: "ok"}}},
	}}); err != nil {
		t.Fatal(err)
	}
	profile, err := app.ParseProfile([]byte(`{"name":"dev","bundles":["base"]}`))
	if err != nil {
		t.Fatal(err)
	}
	ready, err := app.Mount(context.Background(), profile, bundles, plugins)
	if err != nil {
		t.Fatal(err)
	}
	if ready.Tree == nil {
		t.Fatal("a mounted profile owns a tree")
	}
	if err := ready.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !ready.Tree.Closed() {
		t.Fatal("Close must close the owned tree")
	}
}

// TestDisabledRequiredEntryBlocksReadiness covers the branch where a required
// entry exists but has no plugin to run.
func TestDisabledRequiredEntryBlocksReadiness(t *testing.T) {
	plugins := simpleCatalog(t, "ok")
	bundles := app.NewCatalog()
	if err := bundles.Register(app.Bundle{Name: "base", Patches: []app.Patch{
		{ID: "", Insert: []loader.Options{{ID: "a", Name: "ok", Disabled: boolPtr(true)}}},
	}}); err != nil {
		t.Fatal(err)
	}
	profile, err := app.ParseProfile([]byte(`{"name":"dev","bundles":["base"],"required":["a"]}`))
	if err != nil {
		t.Fatal(err)
	}
	ready, err := app.Mount(context.Background(), profile, bundles, plugins)
	if !errors.Is(err, app.ErrRequiredEntryFailed) {
		t.Fatalf("a disabled required entry must block readiness: %v", err)
	}
	defer func() { _ = ready.Close(context.Background()) }()
	if len(ready.Failed) != 1 || ready.Failed[0].ID != "a" {
		t.Fatalf("failed = %+v", ready.Failed)
	}
}

// TestSiblingFailureDoesNotBlockWhenNotRequired pins the non-required branch of a
// plugin that fails to become ready.
func TestSiblingFailureDoesNotBlockWhenNotRequired(t *testing.T) {
	plugins := loader.NewCatalog()
	if err := plugins.Register("flaky", func() cordis.Plugin {
		return cordis.Plugin{Name: "flaky", Apply: func(*cordis.Context, any) (cordis.Cleanup, error) {
			return nil, errors.New("nope")
		}}
	}); err != nil {
		t.Fatal(err)
	}
	if err := plugins.Register("sure", func() cordis.Plugin {
		return cordis.Plugin{Name: "sure", Apply: func(*cordis.Context, any) (cordis.Cleanup, error) { return nil, nil }}
	}); err != nil {
		t.Fatal(err)
	}
	bundles := app.NewCatalog()
	if err := bundles.Register(app.Bundle{Name: "base", Patches: []app.Patch{
		{ID: "", Insert: []loader.Options{{ID: "ok", Name: "sure"}, {ID: "bad", Name: "flaky"}}},
	}}); err != nil {
		t.Fatal(err)
	}
	profile, err := app.ParseProfile([]byte(`{"name":"dev","bundles":["base"],"required":["ok"]}`))
	if err != nil {
		t.Fatal(err)
	}
	ready, err := app.Mount(context.Background(), profile, bundles, plugins)
	if err != nil {
		t.Fatalf("a non-required failure must not block readiness: %v", err)
	}
	defer func() { _ = ready.Close(context.Background()) }()
	if len(ready.Siblings) != 1 || ready.Siblings[0].ID != "bad" {
		t.Fatalf("siblings = %+v", ready.Siblings)
	}
	if len(ready.Failed) != 0 {
		t.Fatalf("failed = %+v", ready.Failed)
	}
}

func boolPtr(value bool) *bool { return &value }

func stringPtr(value string) *string { return &value }
