package preset

import (
	"errors"
	"testing"
)

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	registry := NewRegistry()
	for _, bundle := range []Preset{
		{
			Name: "default", Description: "the base bundle",
			Settings: []Contribution{{Key: "model", Value: "base-model"}, {Key: "verbosity", Value: "1"}},
			Persona:  "You are a careful assistant.",
		},
		{
			Name: "terse", Description: "short answers",
			Settings: []Contribution{{Key: "verbosity", Value: "0"}},
		},
	} {
		if err := registry.Register(bundle); err != nil {
			t.Fatalf("Register(%s): %v", bundle.Name, err)
		}
	}
	return registry
}

func TestApplyResolvesAKnownPreset(t *testing.T) {
	applied, err := testRegistry(t).Apply("default")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if applied.Name != "default" {
		t.Fatalf("Name = %q, want default", applied.Name)
	}
	if len(applied.Settings) != 2 {
		t.Fatalf("Settings = %+v, want two contributions", applied.Settings)
	}
	if applied.Persona == "" {
		t.Fatal("Persona is empty, want the bundle's persona")
	}
	// Sorted by key, so the order is a fact rather than registration luck.
	if applied.Settings[0].Key != "model" {
		t.Fatalf("first key = %q, want model", applied.Settings[0].Key)
	}
}

func TestUnknownPresetIsRefused(t *testing.T) {
	if _, err := testRegistry(t).Apply("missing"); !errors.Is(err, ErrUnknownPreset) {
		t.Fatalf("Apply error = %v, want ErrUnknownPreset", err)
	}
}

func TestNilRegistryIsRefused(t *testing.T) {
	var registry *Registry
	if _, err := registry.Apply("default"); !errors.Is(err, ErrUnknownPreset) {
		t.Fatalf("nil Apply error = %v, want ErrUnknownPreset", err)
	}
	if _, err := registry.Merge("default"); !errors.Is(err, ErrUnknownPreset) {
		t.Fatalf("nil Merge error = %v, want ErrUnknownPreset", err)
	}
	if _, err := registry.Owners("default"); !errors.Is(err, ErrUnknownPreset) {
		t.Fatalf("nil Owners error = %v, want ErrUnknownPreset", err)
	}
}

func TestRegisterRefusesDuplicates(t *testing.T) {
	registry := testRegistry(t)
	err := registry.Register(Preset{Name: "default", Settings: []Contribution{{Key: "k", Value: "v"}}})
	if !errors.Is(err, ErrInvalidPreset) {
		t.Fatalf("duplicate Register error = %v, want ErrInvalidPreset", err)
	}
}

func TestRegisterRefusesInvalidBundles(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(Preset{Settings: []Contribution{{Key: "k", Value: "v"}}}); !errors.Is(err, ErrInvalidPreset) {
		t.Fatalf("nameless Register error = %v, want ErrInvalidPreset", err)
	}
	if err := registry.Register(Preset{Name: "a", Settings: []Contribution{{Value: "v"}}}); !errors.Is(err, ErrInvalidPreset) {
		t.Fatalf("keyless Register error = %v, want ErrInvalidPreset", err)
	}
	if err := registry.Register(Preset{Name: "b", Settings: []Contribution{{Key: "k", Value: "1"}, {Key: "k", Value: "2"}}}); !errors.Is(err, ErrInvalidPreset) {
		t.Fatalf("double-contribution Register error = %v, want ErrInvalidPreset", err)
	}
}

func TestMergeLetsTheLaterBundleWinAndRecordsTheOwner(t *testing.T) {
	registry := testRegistry(t)
	merged, err := registry.Merge("default", "terse")
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	owners, err := registry.Owners("default", "terse")
	if err != nil {
		t.Fatalf("Owners: %v", err)
	}
	if owners["verbosity"] != "terse" {
		t.Fatalf("verbosity owner = %q, want terse", owners["verbosity"])
	}
	if owners["model"] != "default" {
		t.Fatalf("model owner = %q, want default", owners["model"])
	}
	for _, contribution := range merged.Settings {
		if contribution.Key == "verbosity" && contribution.Value != "0" {
			t.Fatalf("verbosity = %q, want the later bundle's value", contribution.Value)
		}
	}
	// Several bundles merge into an unnamed result: one name would mislead.
	if merged.Name != "" {
		t.Fatalf("Name = %q, want empty for a multi-bundle merge", merged.Name)
	}
}

func TestMergeOfOneBundleKeepsItsName(t *testing.T) {
	merged, err := testRegistry(t).Merge("terse")
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if merged.Name != "terse" {
		t.Fatalf("Name = %q, want terse", merged.Name)
	}
}

func TestMergeRefusesAnUnknownName(t *testing.T) {
	if _, err := testRegistry(t).Merge("default", "nope"); !errors.Is(err, ErrUnknownPreset) {
		t.Fatalf("Merge error = %v, want ErrUnknownPreset", err)
	}
}

func TestMergeOfNoBundlesIsEmpty(t *testing.T) {
	merged, err := testRegistry(t).Merge()
	if err != nil {
		t.Fatalf("Merge(): %v", err)
	}
	if len(merged.Settings) != 0 || merged.Persona != "" {
		t.Fatalf("merged = %+v, want empty", merged)
	}
}

func TestApplyCopiesSoCallersCannotMutateTheRegistry(t *testing.T) {
	registry := testRegistry(t)
	applied, err := registry.Apply("default")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	applied.Settings[0].Value = "mutated"
	again, err := registry.Apply("default")
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	for _, contribution := range again.Settings {
		if contribution.Key == applied.Settings[0].Key && contribution.Value == "mutated" {
			t.Fatal("a caller mutated the registry through the returned slice")
		}
	}
}

func TestNamesAreInRegistrationOrder(t *testing.T) {
	names := testRegistry(t).Names()
	if len(names) != 2 || names[0] != "default" || names[1] != "terse" {
		t.Fatalf("Names() = %v, want registration order", names)
	}
}

func TestLookupReportsPresence(t *testing.T) {
	registry := testRegistry(t)
	if _, ok := registry.Lookup("default"); !ok {
		t.Fatal("Lookup(default) reported missing")
	}
	if _, ok := registry.Lookup("absent"); ok {
		t.Fatal("Lookup(absent) reported present")
	}
}

func TestOwnersRefusesAnUnknownName(t *testing.T) {
	if _, err := testRegistry(t).Owners("nope"); !errors.Is(err, ErrUnknownPreset) {
		t.Fatalf("Owners error = %v, want ErrUnknownPreset", err)
	}
}
