// Command loader demonstrates assembling a plugin tree from one JSON document.
//
// It uses the same sequence the dsh-go command uses: parse the document, mount
// the declared plugins, report each entry by outcome, then release the tree. The
// example keeps that sequence visible so a reader can run it and compare the
// printed result with docs/loader/tutorial.md.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/gosomea/deepseek-harness-go/cordis"
	"github.com/gosomea/deepseek-harness-go/loader"
)

// document is the configuration this example assembles. It declares a provider
// that publishes the store service and a consumer that requires it, plus one
// disabled entry that must not run.
const document = `{
  "entries": [
    {"id": "store", "name": "store"},
    {"id": "cache", "name": "cache", "inject": "store"},
    {"id": "off", "name": "store", "disabled": true}
  ]
}`

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// run assembles the document and prints one line per entry plus a summary.
func run(out io.Writer) (result error) {
	catalog, err := pluginCatalog()
	if err != nil {
		return err
	}

	// Parsing comes first, so a malformed document never reaches mounting.
	parsed, err := loader.ParseDocument([]byte(document))
	if err != nil {
		return err
	}

	tree := loader.NewTree(catalog)
	defer func() { result = errors.Join(result, tree.Close(context.Background())) }()
	if err := tree.Mount(parsed); err != nil {
		return err
	}

	declared, err := tree.Dump(loader.Declared)
	if err != nil {
		return err
	}
	running, err := tree.Dump(loader.Running)
	if err != nil {
		return err
	}
	fmt.Fprint(out, "--- declared ---\n", declared)
	fmt.Fprint(out, "--- running ---\n", running)

	// Report entries in sorted order so the output is reproducible.
	paths := tree.Paths()
	sort.Strings(paths)
	active := 0
	for _, path := range paths {
		view, ok := tree.View(path)
		if !ok {
			continue
		}
		state := string(view.State)
		if view.Group {
			state = "group"
		} else if !view.Runnable {
			state = "disabled"
		} else if view.State == cordis.Active {
			active++
		}
		fmt.Fprintf(out, "entry %-12s state=%s\n", path, state)
	}
	fmt.Fprintf(out, "active=%d total=%d\n", active, len(paths))
	return nil
}

// pluginCatalog registers the two plugins the document names. The store plugin
// publishes the service the consumer injects, which is what makes the consumer
// start rather than stay pending.
func pluginCatalog() (*loader.Catalog, error) {
	catalog := loader.NewCatalog()
	if err := catalog.Register("store", func() cordis.Plugin {
		return cordis.Plugin{
			Name: "store",
			Validate: func(config any) (any, error) {
				// The plugin owns its configuration rules. An absent payload means the
				// document did not configure this entry, so the plugin applies its own
				// default rather than the codec inventing one.
				if config == nil {
					return "default", nil
				}
				return config, nil
			},
			Apply: func(ctx *cordis.Context, _ any) (cordis.Cleanup, error) {
				if _, err := ctx.Provide("store", "store-value"); err != nil {
					return nil, err
				}
				return func() error { return nil }, nil
			},
		}
	}); err != nil {
		return nil, err
	}
	if err := catalog.Register("cache", func() cordis.Plugin {
		return cordis.Plugin{
			Name:   "cache",
			Inject: []string{"store"},
			Apply: func(*cordis.Context, any) (cordis.Cleanup, error) {
				return func() error { return nil }, nil
			},
		}
	}); err != nil {
		return nil, err
	}
	return catalog, nil
}
