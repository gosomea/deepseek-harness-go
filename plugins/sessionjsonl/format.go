package sessionjsonl

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gosomea/deepseek-harness-go/persistence"
)

// FormatID is this project's format identity.
//
// It is deliberately not any DSH generation name: the two implementations
// answer different compatibility questions, and an artifact should say which
// one wrote it.
const FormatID = "dsh-go-session"

// FormatVersion is the version this build writes.
const FormatVersion = 1

// LogSuffix is the artifact suffix for a session log.
const LogSuffix = ".jsonl"

// header is the first line of an artifact.
//
// Every field is required: a file without an identity and a version cannot be
// trusted to mean anything, so a header missing either is refused rather than
// defaulted.
type header struct {
	// Format is the format identity.
	Format string `json:"format"`
	// Version is the format version.
	Version int `json:"version"`
	// ID is the session identity the file belongs to.
	ID string `json:"id"`
}

// EncodeHeader renders the header line for a session.
func EncodeHeader(id string) ([]byte, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("%w: header requires a session id", ErrInvalidHeader)
	}
	encoded, err := json.Marshal(header{Format: FormatID, Version: FormatVersion, ID: id})
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

// decodeHeader parses a header line and checks its identity.
//
// An unknown format is refused and an unknown version is refused separately, so
// a caller can tell "this is not our file" from "this is a newer file of ours".
func decodeHeader(data []byte) (header, error) {
	var decoded header
	if err := json.Unmarshal(data, &decoded); err != nil {
		return header{}, fmt.Errorf("%w: %v", ErrInvalidHeader, err)
	}
	if decoded.Format == "" {
		return header{}, fmt.Errorf("%w: header has no format", ErrInvalidHeader)
	}
	if decoded.Format != FormatID {
		return header{}, fmt.Errorf("%w: %s: %w", persistence.ErrUnsupportedVersion, decoded.Format, ErrForeignFormat)
	}
	if decoded.Version == 0 {
		return header{}, fmt.Errorf("%w: header has no version", ErrInvalidHeader)
	}
	if decoded.Version != FormatVersion {
		return header{}, fmt.Errorf("%w: %w: version %d (this build writes %d)", persistence.ErrUnsupportedVersion, ErrUnsupportedVersion, decoded.Version, FormatVersion)
	}
	if strings.TrimSpace(decoded.ID) == "" {
		return header{}, fmt.Errorf("%w: header has no session id", ErrInvalidHeader)
	}
	return decoded, nil
}

// EncodeRecord renders one stored record.
//
// The payload is the event's own JSON, embedded verbatim so a decoder never has
// to re-derive the envelope: the record is a thin wrapper that adds only the
// length-prefixed payload and its checksum.
type record struct {
	// Seq is the event offset, restated at the record level so a truncated tail
	// can be detected without parsing the payload.
	Seq uint64 `json:"seq"`
	// Payload is the event as session.MarshalEvent produced it.
	Payload json.RawMessage `json:"payload"`
	// Checksum is the SHA-256 of the payload, so a damaged record is detected
	// rather than reinterpreted.
	Checksum string `json:"checksum"`
}

// EncodeRecord renders one event as a record line.
func EncodeRecord(seq uint64, payload []byte) ([]byte, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("%w: record requires a payload", ErrInvalidHeader)
	}
	sum := sha256.Sum256(payload)
	encoded, err := json.Marshal(record{Seq: seq, Payload: payload, Checksum: hex.EncodeToString(sum[:])})
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

// decodeRecord parses a record line and verifies its checksum.
func decodeRecord(data []byte) (record, error) {
	var decoded record
	if err := json.Unmarshal(data, &decoded); err != nil {
		return record{}, fmt.Errorf("%w: %v", ErrCorruptRecord, err)
	}
	if len(decoded.Payload) == 0 {
		return record{}, fmt.Errorf("%w: record has no payload", ErrCorruptRecord)
	}
	sum := sha256.Sum256(decoded.Payload)
	if decoded.Checksum != hex.EncodeToString(sum[:]) {
		return record{}, fmt.Errorf("%w: checksum mismatch at seq %d", ErrCorruptRecord, decoded.Seq)
	}
	return decoded, nil
}

// SessionPath returns the artifact path for a session under a data directory.
//
// The id is encoded rather than used verbatim: a SessionID is an unvalidated
// string, so a provider that joined it directly could be talked into writing
// outside the configured directory.
func SessionPath(root string, id string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("%w: data directory is required", ErrInvalidHeader)
	}
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("%w: session id is required", ErrInvalidHeader)
	}
	// The id is encoded, not validated: a SessionID is an unvalidated string, and
	// the reference encodes it for exactly that reason. Rejecting a separator here
	// would refuse ids the reference stores, while a reversible encoding keeps
	// every id addressable as one path element with no traversal.
	return filepath.Join(root, encodeID(id)+LogSuffix), nil
}

// DecodeID recovers the session id from an encoded artifact name.
//
// It exists so a reader can show the identity a listing stands for: an encoding
// that could not be reversed would make the stored session unnameable.
func DecodeID(encoded string) (string, error) {
	var out strings.Builder
	for index := 0; index < len(encoded); index++ {
		char := encoded[index]
		if char != '%' {
			out.WriteByte(char)
			continue
		}
		if index+2 >= len(encoded) {
			return "", fmt.Errorf("%w: truncated escape in %q", ErrInvalidHeader, encoded)
		}
		var value int
		if _, err := fmt.Sscanf(encoded[index+1:index+3], "%02x", &value); err != nil {
			return "", fmt.Errorf("%w: bad escape in %q", ErrInvalidHeader, encoded)
		}
		out.WriteByte(byte(value))
		index += 2
	}
	return out.String(), nil
}

// encodeID makes a session id safe as a path element without losing identity.
//
// It is a reversible encoding, not a hash: two different ids must never collide,
// and a reader must be able to recover the id a listing shows.
func encodeID(id string) string {
	var out strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			out.WriteRune(r)
		default:
			fmt.Fprintf(&out, "%%%02x", r)
		}
	}
	return out.String()
}

// EncodeHeaderAtVersion renders a header line at an arbitrary version.
//
// It exists so a diagnostic — and the scenario runner — can produce the artifact
// a newer build would have written, in order to prove this build refuses it. It
// is deliberately not used by the write path, which always writes FormatVersion.
func EncodeHeaderAtVersion(id string, version int) ([]byte, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("%w: header requires a session id", ErrInvalidHeader)
	}
	if version <= 0 {
		return nil, fmt.Errorf("%w: header version must be positive", ErrInvalidHeader)
	}
	return json.Marshal(header{Format: FormatID, Version: version, ID: id})
}
