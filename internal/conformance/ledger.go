package conformance

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// Errors this package returns.
var (
	// ErrUnknownGroup reports a disposition for a group not in the inventory.
	ErrUnknownGroup = errors.New("conformance: disposition names an unknown group")
	// ErrMissingDisposition reports a group with no disposition.
	ErrMissingDisposition = errors.New("conformance: group has no disposition")
	// ErrInvalidDisposition reports a disposition with an unknown status.
	ErrInvalidDisposition = errors.New("conformance: invalid disposition")
)

// Status is a group's conformance disposition.
type Status string

// The dispositions this package accepts.
const (
	// StatusVerified is behaviour-equivalent with tests and evidence.
	StatusVerified Status = "verified"
	// StatusAdapted is implemented with a documented Go difference.
	StatusAdapted Status = "adapted"
	// StatusGap is not delivered in this environment; a recorded gap.
	StatusGap Status = "gap"
)

// Inventory is the fixed-baseline package inventory.
type Inventory struct {
	// PackageGroups maps a group name to the paths it covers.
	PackageGroups map[string][]string `json:"package_groups"`
	// VendorPackages lists vendor package paths.
	VendorPackages []string `json:"vendor_packages"`
}

// Disposition is one group's recorded outcome.
type Disposition struct {
	// Status is verified, adapted or gap.
	Status Status `json:"status"`
	// Note explains the disposition, required for a gap.
	Note string `json:"note,omitempty"`
	// Owner is the milestone that owns the disposition.
	Owner string `json:"owner,omitempty"`
}

// Ledger is the set of dispositions keyed by group name.
type Ledger struct {
	// Groups maps a group name to its disposition.
	Groups map[string]Disposition `json:"groups"`
}

// LoadInventory parses an inventory.
func LoadInventory(data []byte) (Inventory, error) {
	var inv Inventory
	if err := json.Unmarshal(data, &inv); err != nil {
		return Inventory{}, fmt.Errorf("conformance: parse inventory: %w", err)
	}
	if len(inv.PackageGroups) == 0 {
		return Inventory{}, fmt.Errorf("conformance: inventory has no package groups")
	}
	return inv, nil
}

// LoadLedger parses a ledger.
func LoadLedger(data []byte) (Ledger, error) {
	var ledger Ledger
	if err := json.Unmarshal(data, &ledger); err != nil {
		return Ledger{}, fmt.Errorf("conformance: parse ledger: %w", err)
	}
	return ledger, nil
}

// Names returns every group and vendor name in the inventory, sorted.
func (inv Inventory) Names() []string {
	set := map[string]bool{}
	for name := range inv.PackageGroups {
		set[name] = true
	}
	for _, path := range inv.VendorPackages {
		set[vendorName(path)] = true
	}
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// vendorName turns a vendor path into its group name.
func vendorName(path string) string {
	const prefix = "vendor/"
	if len(path) > len(prefix) && path[:len(prefix)] == prefix {
		return path[len(prefix):]
	}
	return path
}

// hasVendor reports whether a name matches a vendor package.
func (inv Inventory) hasVendor(name string) bool {
	for _, path := range inv.VendorPackages {
		if vendorName(path) == name {
			return true
		}
	}
	return false
}

// Validate reports whether the ledger disposes every inventory name exactly and
// with a valid status.
//
// It checks three things a reader of "full coverage" would assume: no group is
// missing a disposition, no disposition names a group the inventory lacks (a
// typo would otherwise hide a real gap), and every gap carries a note so a gap is
// explained rather than silently counted.
func Validate(inv Inventory, ledger Ledger) error {
	for name, disp := range ledger.Groups {
		if _, ok := inv.PackageGroups[name]; !ok {
			if !inv.hasVendor(name) {
				return fmt.Errorf("%w: %q", ErrUnknownGroup, name)
			}
		}
		switch disp.Status {
		case StatusVerified, StatusAdapted, StatusGap:
		default:
			return fmt.Errorf("%w: %q has status %q", ErrInvalidDisposition, name, disp.Status)
		}
		if disp.Status == StatusGap && disp.Note == "" {
			return fmt.Errorf("%w: gap %q has no note", ErrInvalidDisposition, name)
		}
	}
	for _, name := range inv.Names() {
		if _, ok := ledger.Groups[name]; !ok {
			return fmt.Errorf("%w: %q", ErrMissingDisposition, name)
		}
	}
	return nil
}

// Summarize counts dispositions by status.
func Summarize(ledger Ledger) map[Status]int {
	out := map[Status]int{}
	for _, disp := range ledger.Groups {
		out[disp.Status]++
	}
	return out
}
