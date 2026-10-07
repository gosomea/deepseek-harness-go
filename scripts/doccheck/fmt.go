package main

import (
	"bytes"
	"fmt"
	"go/format"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// checkFormatting reports every Go source file whose content is not gofmt-clean.
//
// The scope is discovered from the repository tree rather than from a list of
// directories: a hardcoded list silently stops covering a package the moment one
// is added, which is exactly when the gate is needed. Directories holding
// generated or historical evidence are skipped for the same reason the other
// documentation checks skip them.
func checkFormatting(root string) error {
	var unformatted []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "bin" || d.Name() == "validation") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		unformattedFile, err := misformatted(path)
		if err != nil {
			return err
		}
		if unformattedFile {
			relative, relErr := filepath.Rel(root, path)
			if relErr != nil {
				relative = path
			}
			unformatted = append(unformatted, filepath.ToSlash(relative))
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(unformatted) == 0 {
		return nil
	}
	// Sorting makes the report reproducible across file-system walk order.
	sort.Strings(unformatted)
	return fmt.Errorf("these files are not gofmt-clean:\n%s", strings.Join(unformatted, "\n"))
}

// misformatted reports whether one file differs from its gofmt rendering, and
// propagates a parse failure: source that cannot be parsed cannot be formatted.
func misformatted(path string) (bool, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	formatted, err := format.Source(source)
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	return !bytes.Equal(source, formatted), nil
}
