package skills

import (
	"errors"
	"strings"
	"testing"
)

func testCatalogue(t *testing.T) *Catalogue {
	t.Helper()
	catalogue := NewCatalogue()
	for _, skill := range []Skill{
		{Name: "base", Description: "the base skill", Body: "base body"},
		{Name: "mid", Description: "depends on base", Body: "mid body", DependsOn: []string{"base"}},
		{Name: "leaf", Description: "depends on mid", Body: "leaf body", DependsOn: []string{"mid"}},
		{Name: "lone", Description: "no dependencies", Body: "lone body"},
	} {
		if err := catalogue.Register(skill); err != nil {
			t.Fatalf("Register(%s): %v", skill.Name, err)
		}
	}
	return catalogue
}

func TestDiscoverListsWithoutBodies(t *testing.T) {
	entries := testCatalogue(t).Discover()
	if len(entries) != 4 {
		t.Fatalf("discovered %d entries, want 4", len(entries))
	}
	// Sorted by name, so registration order does not decide the listing.
	want := []string{"base", "leaf", "lone", "mid"}
	for index, name := range want {
		if entries[index].Name != name {
			t.Fatalf("entry %d = %q, want %q", index, entries[index].Name, name)
		}
	}
	if entries[0].Description == "" {
		t.Fatal("description is empty; a catalogue row needs one")
	}
}

func TestDiscoverOfANilCatalogueIsEmpty(t *testing.T) {
	var catalogue *Catalogue
	if entries := catalogue.Discover(); len(entries) != 0 {
		t.Fatalf("nil catalogue discovered %d entries, want 0", len(entries))
	}
	if names := catalogue.Names(); len(names) != 0 {
		t.Fatalf("nil catalogue names = %v, want empty", names)
	}
	if _, ok := catalogue.Lookup("base"); ok {
		t.Fatal("nil catalogue reported a lookup hit")
	}
}

func TestLoadPullsInDependencies(t *testing.T) {
	loader := NewLoader(testCatalogue(t))
	if err := loader.Load("leaf"); err != nil {
		t.Fatalf("Load(leaf): %v", err)
	}
	names := loader.EnabledNames()
	// Dependencies are enabled before the dependent, so the render order is a
	// valid reading order.
	if strings.Join(names, ",") != "base,mid,leaf" {
		t.Fatalf("enabled = %v, want base,mid,leaf", names)
	}
}

func TestLoadIsIdempotent(t *testing.T) {
	loader := NewLoader(testCatalogue(t))
	if err := loader.Load("base"); err != nil {
		t.Fatalf("first Load: %v", err)
	}
	if err := loader.Load("base"); err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if names := loader.EnabledNames(); len(names) != 1 {
		t.Fatalf("enabled = %v, want one entry after a repeated load", names)
	}
}

func TestLoadRefusesAnUnknownSkill(t *testing.T) {
	loader := NewLoader(testCatalogue(t))
	if err := loader.Load("ghost"); !errors.Is(err, ErrUnknownSkill) {
		t.Fatalf("Load error = %v, want ErrUnknownSkill", err)
	}
}

func TestLoadRefusesWithoutACatalogue(t *testing.T) {
	var loader *Loader
	if err := loader.Load("base"); !errors.Is(err, ErrUnknownSkill) {
		t.Fatalf("nil Load error = %v, want ErrUnknownSkill", err)
	}
	if err := NewLoader(nil).Load("base"); !errors.Is(err, ErrUnknownSkill) {
		t.Fatalf("catalogue-less Load error = %v, want ErrUnknownSkill", err)
	}
}

func TestLoadRefusesAMissingDependency(t *testing.T) {
	catalogue := NewCatalogue()
	if err := catalogue.Register(Skill{
		Name: "needy", Description: "needs an absent skill", Body: "body",
		DependsOn: []string{"absent"},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := NewLoader(catalogue).Load("needy"); !errors.Is(err, ErrMissingDependency) {
		t.Fatalf("Load error = %v, want ErrMissingDependency", err)
	}
}

func TestLoadRefusesADependencyCycle(t *testing.T) {
	catalogue := NewCatalogue()
	for _, skill := range []Skill{
		{Name: "a", Description: "a", Body: "a body", DependsOn: []string{"b"}},
		{Name: "b", Description: "b", Body: "b body", DependsOn: []string{"a"}},
	} {
		if err := catalogue.Register(skill); err != nil {
			t.Fatalf("Register(%s): %v", skill.Name, err)
		}
	}
	if err := NewLoader(catalogue).Load("a"); !errors.Is(err, ErrInvalidSkill) {
		t.Fatalf("Load error = %v, want ErrInvalidSkill", err)
	}
}

func TestLoadedContentOnlyCoversEnabledSkills(t *testing.T) {
	loader := NewLoader(testCatalogue(t))
	if err := loader.Load("base"); err != nil {
		t.Fatalf("Load: %v", err)
	}
	text := loader.Render()
	if !strings.Contains(text, "base body") {
		t.Fatalf("render = %q, want the enabled body", text)
	}
	// The catalogue holds three other skills; none of their bodies may appear.
	for _, absent := range []string{"mid body", "leaf body", "lone body"} {
		if strings.Contains(text, absent) {
			t.Fatalf("render leaked %q from a skill that was never enabled", absent)
		}
	}
}

func TestUnloadRemovesTheSkillAndItsDependents(t *testing.T) {
	loader := NewLoader(testCatalogue(t))
	if err := loader.Load("leaf"); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := loader.Unload("mid"); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	// mid is gone, and so is leaf, which depended on it. A dependent left enabled
	// would contradict its own declared dependency.
	if loader.Enabled("mid") || loader.Enabled("leaf") {
		t.Fatalf("enabled after unload = %v, want mid and leaf removed", loader.EnabledNames())
	}
	if !loader.Enabled("base") {
		t.Fatalf("base was removed although nothing depends on it: %v", loader.EnabledNames())
	}
	if text := loader.Render(); strings.Contains(text, "mid body") || strings.Contains(text, "leaf body") {
		t.Fatalf("render still carries unloaded content: %q", text)
	}
}

func TestUnloadRefusesASkillThatWasNeverEnabled(t *testing.T) {
	loader := NewLoader(testCatalogue(t))
	if err := loader.Unload("base"); !errors.Is(err, ErrNotEnabled) {
		t.Fatalf("Unload error = %v, want ErrNotEnabled", err)
	}
}

func TestUnloadRefusesAnUnknownSkill(t *testing.T) {
	loader := NewLoader(testCatalogue(t))
	if err := loader.Unload("ghost"); !errors.Is(err, ErrUnknownSkill) {
		t.Fatalf("Unload error = %v, want ErrUnknownSkill", err)
	}
	var nilLoader *Loader
	if err := nilLoader.Unload("base"); !errors.Is(err, ErrUnknownSkill) {
		t.Fatalf("nil Unload error = %v, want ErrUnknownSkill", err)
	}
}

func TestRenderIsAPureFunctionOfTheEnabledSet(t *testing.T) {
	loader := NewLoader(testCatalogue(t))
	if err := loader.Load("leaf"); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if first, second := loader.Render(), loader.Render(); first != second {
		t.Fatalf("two renders differ:\n%q\n%q", first, second)
	}
	other := NewLoader(testCatalogue(t))
	if err := other.Load("leaf"); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loader.Render() != other.Render() {
		t.Fatal("two loaders with the same enabled set rendered differently")
	}
}

func TestDependencyClosureReportsTheCost(t *testing.T) {
	loader := NewLoader(testCatalogue(t))
	closure, err := loader.DependencyClosure("leaf")
	if err != nil {
		t.Fatalf("DependencyClosure: %v", err)
	}
	if strings.Join(closure, ",") != "base,leaf,mid" {
		t.Fatalf("closure = %v, want base,leaf,mid", closure)
	}
}

func TestDependencyClosureRefusesUnknownAndMissing(t *testing.T) {
	loader := NewLoader(testCatalogue(t))
	if _, err := loader.DependencyClosure("ghost"); !errors.Is(err, ErrUnknownSkill) {
		t.Fatalf("unknown closure error = %v, want ErrUnknownSkill", err)
	}
	var nilLoader *Loader
	if _, err := nilLoader.DependencyClosure("base"); !errors.Is(err, ErrUnknownSkill) {
		t.Fatalf("nil closure error = %v, want ErrUnknownSkill", err)
	}
	catalogue := NewCatalogue()
	if err := catalogue.Register(Skill{Name: "needy", Description: "d", Body: "b", DependsOn: []string{"absent"}}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if _, err := NewLoader(catalogue).DependencyClosure("needy"); !errors.Is(err, ErrMissingDependency) {
		t.Fatalf("missing closure error = %v, want ErrMissingDependency", err)
	}
}

func TestRegisterRefusesInvalidAndDuplicateSkills(t *testing.T) {
	catalogue := NewCatalogue()
	if err := catalogue.Register(Skill{Description: "d", Body: "b"}); !errors.Is(err, ErrInvalidSkill) {
		t.Fatalf("nameless Register error = %v, want ErrInvalidSkill", err)
	}
	if err := catalogue.Register(Skill{Name: "a", Body: "b"}); !errors.Is(err, ErrInvalidSkill) {
		t.Fatalf("description-less Register error = %v, want ErrInvalidSkill", err)
	}
	if err := catalogue.Register(Skill{Name: "a", Description: "d"}); !errors.Is(err, ErrInvalidSkill) {
		t.Fatalf("body-less Register error = %v, want ErrInvalidSkill", err)
	}
	if err := catalogue.Register(Skill{Name: "a", Description: "d", Body: "b", DependsOn: []string{"a"}}); !errors.Is(err, ErrInvalidSkill) {
		t.Fatalf("self-dependency Register error = %v, want ErrInvalidSkill", err)
	}
	if err := catalogue.Register(Skill{Name: "a", Description: "d", Body: "b", DependsOn: []string{"x", "x"}}); !errors.Is(err, ErrInvalidSkill) {
		t.Fatalf("repeated-dependency Register error = %v, want ErrInvalidSkill", err)
	}
	if err := catalogue.Register(Skill{Name: "a", Description: "d", Body: "b", DependsOn: []string{" "}}); !errors.Is(err, ErrInvalidSkill) {
		t.Fatalf("empty-dependency Register error = %v, want ErrInvalidSkill", err)
	}
	if err := catalogue.Register(Skill{Name: "a", Description: "d", Body: "b"}); err != nil {
		t.Fatalf("valid Register: %v", err)
	}
	if err := catalogue.Register(Skill{Name: "a", Description: "d", Body: "b"}); !errors.Is(err, ErrDuplicateSkill) {
		t.Fatalf("duplicate Register error = %v, want ErrDuplicateSkill", err)
	}
}

func TestLookupAndEnabledOnNilAreSafe(t *testing.T) {
	var loader *Loader
	if loader.Enabled("base") {
		t.Fatal("nil Loader reported a skill enabled")
	}
	if got := loader.Loaded(); got != nil {
		t.Fatalf("nil Loader Loaded() = %v, want nil", got)
	}
	if names := loader.EnabledNames(); names != nil {
		t.Fatalf("nil Loader EnabledNames() = %v, want nil", names)
	}
}

func TestUnloadDoesNotRemoveAnUnrelatedSkill(t *testing.T) {
	loader := NewLoader(testCatalogue(t))
	for _, name := range []string{"base", "lone"} {
		if err := loader.Load(name); err != nil {
			t.Fatalf("Load(%s): %v", name, err)
		}
	}
	if err := loader.Unload("base"); err != nil {
		t.Fatalf("Unload: %v", err)
	}
	if !loader.Enabled("lone") {
		t.Fatalf("lone was removed although it does not depend on base: %v", loader.EnabledNames())
	}
}
