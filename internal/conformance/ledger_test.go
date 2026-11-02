package conformance_test

import (
	"errors"
	"testing"

	"github.com/gosomea/deepseek-harness-go/internal/conformance"
)

func inv() conformance.Inventory {
	return conformance.Inventory{
		PackageGroups:  map[string][]string{"core": {"packages/core"}, "llm": {"packages/llm"}},
		VendorPackages: []string{"vendor/cordis"},
	}
}

func TestNamesIncludeVendor(t *testing.T) {
	names := inv().Names()
	if len(names) != 3 {
		t.Fatalf("names = %v", names)
	}
	// sorted lexically: cordis, core, llm
	if names[0] != "cordis" || names[1] != "core" || names[2] != "llm" {
		t.Fatalf("names = %v", names)
	}
}

func TestValidateOK(t *testing.T) {
	ledger := conformance.Ledger{Groups: map[string]conformance.Disposition{
		"core":   {Status: conformance.StatusVerified},
		"llm":    {Status: conformance.StatusAdapted},
		"cordis": {Status: conformance.StatusAdapted},
	}}
	if err := conformance.Validate(inv(), ledger); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateMissing(t *testing.T) {
	ledger := conformance.Ledger{Groups: map[string]conformance.Disposition{
		"core":   {Status: conformance.StatusVerified},
		"cordis": {Status: conformance.StatusAdapted},
	}}
	if err := conformance.Validate(inv(), ledger); !errors.Is(err, conformance.ErrMissingDisposition) {
		t.Fatalf("missing err = %v", err)
	}
}

func TestValidateUnknownGroup(t *testing.T) {
	ledger := conformance.Ledger{Groups: map[string]conformance.Disposition{
		"core":   {Status: conformance.StatusVerified},
		"llm":    {Status: conformance.StatusAdapted},
		"cordis": {Status: conformance.StatusAdapted},
		"bogus":  {Status: conformance.StatusVerified},
	}}
	if err := conformance.Validate(inv(), ledger); !errors.Is(err, conformance.ErrUnknownGroup) {
		t.Fatalf("unknown err = %v", err)
	}
}

func TestValidateGapNeedsNote(t *testing.T) {
	ledger := conformance.Ledger{Groups: map[string]conformance.Disposition{
		"core":   {Status: conformance.StatusVerified},
		"llm":    {Status: conformance.StatusGap},
		"cordis": {Status: conformance.StatusAdapted},
	}}
	if err := conformance.Validate(inv(), ledger); !errors.Is(err, conformance.ErrInvalidDisposition) {
		t.Fatalf("gap no note err = %v", err)
	}
}

func TestValidateInvalidStatus(t *testing.T) {
	ledger := conformance.Ledger{Groups: map[string]conformance.Disposition{
		"core":   {Status: "bogus"},
		"llm":    {Status: conformance.StatusAdapted},
		"cordis": {Status: conformance.StatusAdapted},
	}}
	if err := conformance.Validate(inv(), ledger); !errors.Is(err, conformance.ErrInvalidDisposition) {
		t.Fatalf("invalid status err = %v", err)
	}
}

func TestSummarizeAndLoad(t *testing.T) {
	summary := conformance.Summarize(conformance.Ledger{Groups: map[string]conformance.Disposition{
		"a": {Status: conformance.StatusVerified},
		"b": {Status: conformance.StatusGap, Note: "x"},
	}})
	if summary[conformance.StatusVerified] != 1 || summary[conformance.StatusGap] != 1 {
		t.Fatalf("summary = %v", summary)
	}
	if _, err := conformance.LoadInventory([]byte(`{"package_groups":{}}`)); err == nil {
		t.Fatal("empty inventory should error")
	}
	if _, err := conformance.LoadInventory([]byte(`bad`)); err == nil {
		t.Fatal("bad json should error")
	}
	if _, err := conformance.LoadLedger([]byte(`bad`)); err == nil {
		t.Fatal("bad ledger json should error")
	}
}
