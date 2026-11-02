package settings

import (
	"errors"
	"testing"
)

func testSchema(t *testing.T) *Schema {
	t.Helper()
	schema := NewSchema()
	for _, declaration := range []Declaration{
		{Key: "model", Kind: KindString, Default: Value{String: "base-model"}},
		{Key: "verbosity", Kind: KindInt, Default: Value{Int: 1}},
		{Key: "streaming", Kind: KindBool, Default: Value{Bool: true}},
	} {
		if err := schema.Declare(declaration); err != nil {
			t.Fatalf("Declare(%s): %v", declaration.Key, err)
		}
	}
	return schema
}

func TestDefaultAppliesWhenNoLayerOverrides(t *testing.T) {
	registry := NewRegistry(testSchema(t), "root")
	resolved, err := registry.Resolve("root", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	model, err := registry.Effective("root", "", "model")
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	if model.Value.String != "base-model" || model.Source != SourceDefault {
		t.Fatalf("model = %+v, want the schema default", model)
	}
	if len(resolved) != 3 {
		t.Fatalf("resolved %d keys, want 3", len(resolved))
	}
}

func TestScopeOverrideBeatsTheDefault(t *testing.T) {
	registry := NewRegistry(testSchema(t), "root")
	registry.Layer("root").Set("model", Value{String: "root-model"})
	effective, err := registry.Effective("root", "", "model")
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	if effective.Value.String != "root-model" || effective.Source != SourceScope || effective.Scope != "root" {
		t.Fatalf("effective = %+v, want the root scope value", effective)
	}
}

func TestNearerScopeOverridesTheFartherOne(t *testing.T) {
	registry := NewRegistry(testSchema(t), "root")
	if err := registry.SetParent("child", "root"); err != nil {
		t.Fatalf("SetParent: %v", err)
	}
	registry.Layer("root").Set("model", Value{String: "root-model"})
	registry.Layer("child").Set("model", Value{String: "child-model"})

	effective, err := registry.Effective("child", "", "model")
	if err != nil {
		t.Fatalf("Effective(child): %v", err)
	}
	if effective.Value.String != "child-model" || effective.Scope != "child" {
		t.Fatalf("child effective = %+v, want the child value", effective)
	}
	// The parent keeps its own answer: overriding must not leak downward-up.
	parent, err := registry.Effective("root", "", "model")
	if err != nil {
		t.Fatalf("Effective(root): %v", err)
	}
	if parent.Value.String != "root-model" {
		t.Fatalf("root effective = %+v, want the root value", parent)
	}
}

func TestSessionOverrideBeatsEveryScope(t *testing.T) {
	registry := NewRegistry(testSchema(t), "root")
	if err := registry.SetParent("child", "root"); err != nil {
		t.Fatalf("SetParent: %v", err)
	}
	registry.Layer("child").Set("model", Value{String: "child-model"})
	registry.Session("s-1")["model"] = Value{String: "session-model"}

	effective, err := registry.Effective("child", "s-1", "model")
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	if effective.Value.String != "session-model" || effective.Source != SourceSession {
		t.Fatalf("effective = %+v, want the session value", effective)
	}
}

func TestScopeOverrideDoesNotLeakToASibling(t *testing.T) {
	registry := NewRegistry(testSchema(t), "root")
	for _, scope := range []string{"left", "right"} {
		if err := registry.SetParent(scope, "root"); err != nil {
			t.Fatalf("SetParent(%s): %v", scope, err)
		}
	}
	registry.Layer("left").Set("model", Value{String: "left-model"})

	right, err := registry.Effective("right", "", "model")
	if err != nil {
		t.Fatalf("Effective(right): %v", err)
	}
	if right.Value.String != "base-model" || right.Source != SourceDefault {
		t.Fatalf("right effective = %+v, want the default (no leak from the sibling)", right)
	}
}

func TestResolutionOrderIsRootThenTargetThenSession(t *testing.T) {
	registry := NewRegistry(testSchema(t), "root")
	if err := registry.SetParent("mid", "root"); err != nil {
		t.Fatalf("SetParent(mid): %v", err)
	}
	if err := registry.SetParent("leaf", "mid"); err != nil {
		t.Fatalf("SetParent(leaf): %v", err)
	}
	path, err := ScopePath("root", registry.parents, "leaf")
	if err != nil {
		t.Fatalf("ScopePath: %v", err)
	}
	if len(path) != 3 || path[0] != "root" || path[1] != "mid" || path[2] != "leaf" {
		t.Fatalf("path = %v, want root-first order", path)
	}
}

func TestScopePathRefusesADisconnectedTarget(t *testing.T) {
	if _, err := ScopePath("root", map[string]string{}, "orphan"); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("ScopePath error = %v, want ErrInvalidSchema", err)
	}
}

func TestScopePathRefusesACycle(t *testing.T) {
	parents := map[string]string{"a": "b", "b": "a"}
	if _, err := ScopePath("root", parents, "a"); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("ScopePath error = %v, want ErrInvalidSchema", err)
	}
}

func TestSetParentRefusesRepointing(t *testing.T) {
	registry := NewRegistry(testSchema(t), "root")
	if err := registry.SetParent("child", "root"); err != nil {
		t.Fatalf("first SetParent: %v", err)
	}
	if err := registry.SetParent("child", "other"); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("second SetParent error = %v, want ErrInvalidSchema", err)
	}
}

func TestUnknownKeyIsRefusedNotDefaulted(t *testing.T) {
	registry := NewRegistry(testSchema(t), "root")
	registry.Layer("root").Set("model", Value{String: "x"})
	registry.Layer("root").Set("typo-key", Value{String: "y"})
	if _, err := registry.Resolve("root", ""); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("Resolve error = %v, want ErrUnknownKey", err)
	}
}

func TestUnknownSessionKeyIsRefused(t *testing.T) {
	registry := NewRegistry(testSchema(t), "root")
	registry.Session("s-1")["nope"] = Value{String: "x"}
	if _, err := registry.Resolve("root", "s-1"); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("Resolve error = %v, want ErrUnknownKey", err)
	}
}

func TestWrongKindIsRefused(t *testing.T) {
	registry := NewRegistry(testSchema(t), "root")
	// verbosity is an int; a string value must not be accepted.
	registry.Layer("root").Set("verbosity", Value{String: "loud"})
	if _, err := registry.Resolve("root", ""); !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("Resolve error = %v, want ErrInvalidValue", err)
	}
}

func TestSchemaRefusesDuplicatesAndBadDefaults(t *testing.T) {
	schema := NewSchema()
	if err := schema.Declare(Declaration{Key: "k", Kind: KindString}); err != nil {
		t.Fatalf("Declare: %v", err)
	}
	if err := schema.Declare(Declaration{Key: "k", Kind: KindString}); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("duplicate Declare error = %v, want ErrInvalidSchema", err)
	}
	if err := schema.Declare(Declaration{Key: "", Kind: KindString}); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("empty-key Declare error = %v, want ErrInvalidSchema", err)
	}
	if err := schema.Declare(Declaration{Key: "bad", Kind: Kind("mystery")}); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("unknown-kind Declare error = %v, want ErrInvalidSchema", err)
	}
	// A default that violates its own kind makes every later message suspect.
	if err := schema.Declare(Declaration{Key: "oops", Kind: KindInt, Default: Value{String: "text"}}); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("bad-default Declare error = %v, want ErrInvalidSchema", err)
	}
}

func TestResolveRefusesANilSchemaAndRegistry(t *testing.T) {
	if _, err := Resolve(nil, []string{""}, nil, nil); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("Resolve(nil) error = %v, want ErrInvalidSchema", err)
	}
	var registry *Registry
	if _, err := registry.Resolve("root", ""); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("nil Registry error = %v, want ErrInvalidSchema", err)
	}
}

func TestEffectiveReportsUnknownKey(t *testing.T) {
	registry := NewRegistry(testSchema(t), "root")
	if _, err := registry.Effective("root", "", "missing"); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("Effective error = %v, want ErrUnknownKey", err)
	}
}

func TestResolvedOutputIsSortedByKey(t *testing.T) {
	registry := NewRegistry(testSchema(t), "root")
	resolved, err := registry.Resolve("root", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for index := 1; index < len(resolved); index++ {
		if resolved[index-1].Key >= resolved[index].Key {
			t.Fatalf("keys are not sorted: %v", resolved)
		}
	}
}

func TestValueDisplay(t *testing.T) {
	got := (Value{String: "s", Bool: true, Int: 3}).Display()
	if got == "" {
		t.Fatal("Display returned an empty string")
	}
}

func TestSchemaKeysAndLookup(t *testing.T) {
	schema := testSchema(t)
	keys := schema.Keys()
	if len(keys) != 3 || keys[0] != "model" {
		t.Fatalf("Keys() = %v, want declaration order", keys)
	}
	if _, ok := schema.Lookup("model"); !ok {
		t.Fatal("Lookup(model) reported missing")
	}
	if _, ok := schema.Lookup("absent"); ok {
		t.Fatal("Lookup(absent) reported present")
	}
}

func TestNilLayerIsSkipped(t *testing.T) {
	registry := NewRegistry(testSchema(t), "root")
	registry.layers["root"] = nil
	effective, err := registry.Effective("root", "", "model")
	if err != nil {
		t.Fatalf("Effective: %v", err)
	}
	if effective.Source != SourceDefault {
		t.Fatalf("effective = %+v, want the default", effective)
	}
}
