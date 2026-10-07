package loader

import (
	"encoding/json"
	"fmt"

	"github.com/gosomea/deepseek-harness-go/cordis"
)

// Entry is one configured node after its name resolved to a plugin and its raw
// configuration payload was validated. It pairs the document's identity with the
// concrete plugin, so later slices can mount it without re-reading the document.
//
// An Entry's declared fields are fixed once resolved. Which group it belongs to
// changes on a move, so that one field is mutable and owned by the tree.
type Entry struct {
	options    Options
	plugin     cordis.Plugin
	config     any
	resolvedOK bool
	// group is the group this entry currently belongs to. A move changes it while
	// the entry's own identity (its path) stays the source of truth.
	group *Group
}

// ID is the entry's identity inside its containing tree. A move changes it,
// because an entry's full path is what addresses it.
func (e *Entry) ID() string { return e.options.ID }

// setPath records the entry's new path after a move.
func (e *Entry) setPath(path string) { e.options.ID = path }

// Name is the catalog name of the entry's plugin.
func (e *Entry) Name() string { return e.options.Name }

// Disabled reports whether the document disabled this entry. A disabled entry is
// still a valid entry: it resolves, but carries no plugin to run.
func (e *Entry) Disabled() bool { return e.options.IsDisabled() }

// IsGroup reports whether this entry groups other entries instead of naming a
// plugin.
func (e *Entry) IsGroup() bool { return e.options.IsGroup() }

// Options returns the raw document values this entry came from. The returned
// slice and byte slices are copies, so a caller cannot mutate the entry.
func (e *Entry) Options() Options {
	copied := e.options
	copied.Inject = append(Inject(nil), e.options.Inject...)
	if e.options.Config != nil {
		copied.Config = append(json.RawMessage(nil), e.options.Config...)
	}
	return copied
}

// Plugin returns the plugin built for this entry. A disabled or group entry has
// no plugin, so the zero value is returned and Runnable reports false.
func (e *Entry) Plugin() cordis.Plugin { return e.plugin }

// Config returns the validated configuration produced by the plugin's own
// Validate. It is nil for a disabled or group entry.
func (e *Entry) Config() any { return e.config }

// Runnable reports whether this entry has a plugin to apply. A disabled entry
// and a group entry both report false, for different reasons that
// Disabled and IsGroup distinguish.
func (e *Entry) Runnable() bool { return e.resolvedOK }

// Resolve turns parsed options into entries by looking each name up in the
// catalog and letting the plugin validate its own raw config. It performs no
// side effects: nothing is mounted, applied or registered.
//
// The three failure classes stay distinguishable: an unknown name reports
// ErrUnknownPlugin, a configuration the plugin rejects reports ErrInvalidConfig,
// and a malformed document was already rejected by ParseDocument with
// ErrDuplicateID or ErrInvalidConfig.
func Resolve(document *Document, catalog *Catalog) ([]*Entry, error) {
	if document == nil {
		return nil, fmt.Errorf("%w: document is nil", ErrInvalidConfig)
	}
	if catalog == nil {
		return nil, fmt.Errorf("%w: catalog is nil", ErrInvalidConfig)
	}
	entries := make([]*Entry, 0, len(document.Entries))
	for _, options := range document.Entries {
		entry := &Entry{options: options}
		// A disabled entry and a group entry are both valid without a plugin.
		// They still get an Entry so later slices can show why they do not run.
		if options.IsDisabled() || options.IsGroup() {
			entries = append(entries, entry)
			continue
		}
		factory, err := catalog.Lookup(options.Name)
		if err != nil {
			return nil, fmt.Errorf("entry %q: %w", options.ID, err)
		}
		plugin := factory()
		if plugin.Name == "" || plugin.Apply == nil {
			return nil, fmt.Errorf("%w: entry %q factory produced an incomplete plugin", ErrInvalidConfig, options.ID)
		}
		config, err := decodeConfig(options.Config)
		if err != nil {
			return nil, fmt.Errorf("entry %q: %w", options.ID, err)
		}
		// A plugin without Validate accepts its configuration unchanged; calling a
		// nil function here would panic instead of reporting a usable entry.
		validated := config
		if plugin.Validate != nil {
			validated, err = plugin.Validate(config)
			if err != nil {
				return nil, fmt.Errorf("entry %q: %w", options.ID, err)
			}
		}
		entry.plugin = plugin
		entry.config = validated
		entry.resolvedOK = true
		entries = append(entries, entry)
	}
	return entries, nil
}

// decodeConfig converts an entry's raw payload into the value handed to a
// plugin's Validate. An absent payload becomes nil, which a plugin may accept or
// reject; the codec does not invent a default.
func decodeConfig(raw json.RawMessage) (any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("%w: config: %s", ErrInvalidConfig, err)
	}
	return value, nil
}

// Load parses a document and resolves every entry against the catalog.
func Load(data []byte, catalog *Catalog) ([]*Entry, error) {
	document, err := ParseDocument(data)
	if err != nil {
		return nil, err
	}
	return Resolve(document, catalog)
}
