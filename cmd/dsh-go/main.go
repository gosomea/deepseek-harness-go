// Command dsh-go inspects a loader configuration document without running a task.
//
// It is the host's configuration entry: the same parse, resolve and mount path
// the application uses, exposed as a check a build or a reviewer can run.
// Inspecting mounts the declared plugins and then releases them, so the command
// leaves no resource behind and prints a result that a repeat run reproduces.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/gosomea/deepseek-harness-go/cordis"
	"github.com/gosomea/deepseek-harness-go/loader"
)

// exitUsage reports a rejected command line or an unreadable input.
const exitUsage = 2

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "dsh-go:", err)
		var usage usageError
		if errors.As(err, &usage) {
			os.Exit(exitUsage)
		}
		os.Exit(1)
	}
}

// usageError marks a rejected command line or input, which exits with exitUsage.
type usageError struct{ err error }

// Error reports the wrapped reason.
func (u usageError) Error() string { return u.err.Error() }

// Unwrap exposes the wrapped reason to errors.Is.
func (u usageError) Unwrap() error { return u.err }

// options is one parsed command line.
type options struct {
	// config is the path of the configuration document to inspect.
	config string
	// plugins lists the catalog names the document may reference.
	plugins []string
	// mode selects check or dump.
	mode string
}

// parse reads a command line, rejecting a missing or contradictory option.
func parse(args []string, stderr io.Writer) (options, error) {
	flags := flag.NewFlagSet("dsh-go", flag.ContinueOnError)
	flags.SetOutput(stderr)
	config := flags.String("config", "", "path of the loader configuration document")
	plugins := flags.String("plugin", "", "comma-separated plugin names the document may reference")
	mode := flags.String("mode", "check", "check the document, or dump the composed tree (check|dump)")
	if err := flags.Parse(args); err != nil {
		return options{}, usageError{err}
	}
	if strings.TrimSpace(*config) == "" {
		return options{}, usageError{errors.New("-config is required")}
	}
	if *mode != "check" && *mode != "dump" {
		return options{}, usageError{fmt.Errorf("unknown mode %q: use check or dump", *mode)}
	}
	return options{config: *config, plugins: splitList(*plugins), mode: *mode}, nil
}

// splitList splits a comma-separated flag value, rejecting empty entries.
func splitList(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// pluginCatalog builds the catalog a document resolves against. A document names
// plugins by string, so the host states which names exist; every name becomes a
// plugin that acquires nothing.
func pluginCatalog(names []string) (*loader.Catalog, error) {
	catalog := loader.NewCatalog()
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if seen[name] {
			return nil, usageError{fmt.Errorf("plugin %q is listed twice", name)}
		}
		seen[name] = true
		pluginName := name
		if err := catalog.Register(name, func() cordis.Plugin {
			return cordis.Plugin{
				Name: pluginName,
				Apply: func(*cordis.Context, any) (cordis.Cleanup, error) {
					return nil, nil
				},
			}
		}); err != nil {
			return nil, usageError{err}
		}
	}
	return catalog, nil
}

// run executes one command against the supplied streams.
func run(args []string, stdout, stderr io.Writer) error {
	opts, err := parse(args, stderr)
	if err != nil {
		return err
	}
	document, err := os.ReadFile(opts.config)
	if err != nil {
		return usageError{fmt.Errorf("reading %s: %w", opts.config, err)}
	}
	plugins, err := pluginCatalog(opts.plugins)
	if err != nil {
		return err
	}
	if opts.mode == "dump" {
		return dump(document, plugins, stdout)
	}
	return check(document, plugins, stdout)
}

// loadTree mounts a configuration document and returns the running tree. This is
// the assembly path the CLI and examples share: parse first, mount second, and
// the caller owns the returned tree.
func loadTree(document []byte, plugins *loader.Catalog) (*loader.Tree, error) {
	tree := loader.NewTree(plugins)
	documentParsed, err := loader.ParseDocument(document)
	if err != nil {
		_ = tree.Close(context.Background())
		return nil, err
	}
	if err := tree.Mount(documentParsed); err != nil {
		_ = tree.Close(context.Background())
		return nil, err
	}
	return tree, nil
}

// check mounts a document, reports every entry by outcome, and releases the tree.
// The output is sorted, so a repeat run reproduces it exactly.
func check(document []byte, plugins *loader.Catalog, out io.Writer) error {
	tree, err := loadTree(document, plugins)
	if err != nil {
		return err
	}

	active := make([]string, 0, 4)
	pending := make([]string, 0, 4)
	unmounted := make([]string, 0, 4)
	for _, path := range tree.Paths() {
		view, ok := tree.View(path)
		if !ok {
			continue
		}
		switch {
		case view.Group:
			// A group entry carries no instance of its own.
		case view.Err != nil:
			unmounted = append(unmounted, fmt.Sprintf("%s failed: %v", path, view.Err))
		case !view.Runnable:
			unmounted = append(unmounted, path+" disabled")
		case view.State == cordis.Active:
			active = append(active, path)
		case view.State == cordis.Pending:
			pending = append(pending, path)
		default:
			unmounted = append(unmounted, fmt.Sprintf("%s is %s", path, view.State))
		}
	}
	sort.Strings(active)
	sort.Strings(pending)
	sort.Strings(unmounted)

	for _, path := range active {
		fmt.Fprintf(out, "active %s\n", path)
	}
	for _, path := range pending {
		fmt.Fprintf(out, "pending %s\n", path)
	}
	for _, line := range unmounted {
		fmt.Fprintf(out, "unmounted %s\n", line)
	}
	fmt.Fprintf(out, "summary active=%d pending=%d unmounted=%d\n", len(active), len(pending), len(unmounted))

	// The diagnostic list is part of the result, so a broken tree is never
	// reported as healthy; closing releases every mounted instance.
	lines := tree.Diagnose()
	if err := tree.Close(context.Background()); err != nil {
		return err
	}
	if len(lines) != 0 {
		return fmt.Errorf("configuration problems: %s", strings.Join(lines, "; "))
	}
	return nil
}

// dump prints the tree's declared shape in loader's own format, which is what a
// reviewer compares against a document.
func dump(document []byte, plugins *loader.Catalog, out io.Writer) error {
	tree, err := loadTree(document, plugins)
	if err != nil {
		return err
	}
	text, err := tree.Dump(loader.Declared)
	if err != nil {
		_ = tree.Close(context.Background())
		return err
	}
	if _, err := io.WriteString(out, text); err != nil {
		_ = tree.Close(context.Background())
		return err
	}
	return tree.Close(context.Background())
}
