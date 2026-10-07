package loader

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrDuplicateID reports two entries in one document sharing an id. An id is the
// entry's identity inside its tree, so a duplicate cannot be resolved later.
var ErrDuplicateID = errors.New("loader: duplicate entry id")

// Options is one entry exactly as the configuration document states it: the
// plugin name, the raw config payload and the entry's own flags. It carries no
// validated plugin or decoded config, so writing a document back does not lose
// the original form.
type Options struct {
	// ID is the stable identity of this entry inside its containing tree.
	ID string `json:"id"`
	// Name is the catalog name of the plugin to build.
	Name string `json:"name"`
	// Config holds the plugin's raw configuration payload. It stays encoded so a
	// plugin validates its own configuration rather than the codec guessing.
	Config json.RawMessage `json:"config,omitempty"`
	// Group marks this entry as a nested group of entries rather than a plugin.
	Group *bool `json:"group,omitempty"`
	// Disabled stops this entry from running. Absent and false are equivalent, so
	// the field is a pointer to keep write-back byte-faithful.
	Disabled *bool `json:"disabled,omitempty"`
	// Inject lists services this entry's plugin requires.
	Inject Inject `json:"inject,omitempty"`
}

// Inject is the dependency list of one entry. A document may state a single
// service as a string or several as an array.
type Inject []string

// UnmarshalJSON accepts either a single service name or an array of names.
func (i *Inject) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		*i = nil
		return nil
	}
	if trimmed[0] == '"' {
		var single string
		if err := json.Unmarshal(trimmed, &single); err != nil {
			return err
		}
		if single == "" {
			return fmt.Errorf("%w: inject string is empty", ErrInvalidConfig)
		}
		*i = Inject{single}
		return nil
	}
	var many []string
	if err := json.Unmarshal(trimmed, &many); err != nil {
		return err
	}
	for _, name := range many {
		if name == "" {
			return fmt.Errorf("%w: inject list contains an empty name", ErrInvalidConfig)
		}
	}
	*i = many
	return nil
}

// IsDisabled reports the entry's disabled flag with absent read as false.
func (o Options) IsDisabled() bool { return o.Disabled != nil && *o.Disabled }

// IsGroup reports whether this entry is a nested group rather than a plugin.
func (o Options) IsGroup() bool { return o.Group != nil && *o.Group }

// Document is a parsed configuration document. It is the raw side of the codec:
// every field is the value the document stated, with no plugin constructed yet.
type Document struct {
	// Entries are the configured nodes in document order.
	Entries []Options `json:"entries"`
}

// ParseDocument decodes a configuration document. Unknown fields and wrong value
// types are rejected, so a typo fails at load instead of silently dropping a
// setting. Structural rejections are separate from resolving plugin names.
func ParseDocument(data []byte) (*Document, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var document Document
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrInvalidConfig, describeJSONError(err))
	}
	if err := decoder.Decode(new(struct{})); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: document has trailing content", ErrInvalidConfig)
	}
	if len(document.Entries) == 0 {
		return nil, fmt.Errorf("%w: document has no entries", ErrInvalidConfig)
	}
	seen := make(map[string]bool, len(document.Entries))
	for index, options := range document.Entries {
		if strings.TrimSpace(options.ID) == "" {
			return nil, fmt.Errorf("%w: entry %d has no id", ErrInvalidConfig, index)
		}
		if strings.TrimSpace(options.Name) == "" && !options.IsGroup() {
			return nil, fmt.Errorf("%w: entry %q has no name", ErrInvalidConfig, options.ID)
		}
		if seen[options.ID] {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateID, options.ID)
		}
		seen[options.ID] = true
	}
	return &document, nil
}

// describeJSONError turns the decoder's wording into a stable, field-pointing
// diagnostic. Callers match on the wrapping sentinel, not on this text.
func describeJSONError(err error) string {
	var typeError *json.UnmarshalTypeError
	if errors.As(err, &typeError) {
		field := typeError.Field
		if field == "" {
			field = "document"
		}
		return fmt.Sprintf("field %s: expected %s, got %s", field, typeError.Type, typeError.Value)
	}
	return err.Error()
}
