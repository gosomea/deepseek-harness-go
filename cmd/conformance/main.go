// Command conformance validates that every source package group and vendor
// package has a disposition, and prints a summary.
//
// It exits non-zero when any group is unassigned, a disposition names a group the
// inventory lacks, or a gap has no note — so "full coverage" cannot be claimed
// while the ledger disagrees.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/gosomea/deepseek-harness-go/internal/conformance"
)

func main() {
	inventoryPath := flag.String("inventory", "", "path to source-inventory.json")
	dispositionsPath := flag.String("dispositions", "", "path to dispositions.json")
	flag.Parse()
	if err := run(*inventoryPath, *dispositionsPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(inventoryPath, dispositionsPath string) error {
	invData, err := os.ReadFile(inventoryPath)
	if err != nil {
		return err
	}
	dispositionsData, err := os.ReadFile(dispositionsPath)
	if err != nil {
		return err
	}
	inventory, err := conformance.LoadInventory(invData)
	if err != nil {
		return err
	}
	ledger, err := conformance.LoadLedger(dispositionsData)
	if err != nil {
		return err
	}
	if err := conformance.Validate(inventory, ledger); err != nil {
		return err
	}
	summary := conformance.Summarize(ledger)
	fmt.Printf("conformance ledger: OK (%d groups, verified=%d adapted=%d gap=%d)\n",
		len(ledger.Groups), summary[conformance.StatusVerified], summary[conformance.StatusAdapted], summary[conformance.StatusGap])
	return nil
}
