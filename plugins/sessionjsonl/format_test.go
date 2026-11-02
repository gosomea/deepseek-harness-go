package sessionjsonl

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// A header carries the identity this build writes, and a foreign or newer one is
// refused separately so a caller can tell the two conditions apart.
func TestHeaderRoundTripAndRefusals(t *testing.T) {
	encoded, err := EncodeHeader("s-1")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := decodeHeader(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Format != FormatID || decoded.Version != FormatVersion || decoded.ID != "s-1" {
		t.Fatalf("unexpected header %+v", decoded)
	}

	cases := []struct {
		name string
		line string
		want error
	}{
		{"foreign format", `{"format":"dsh-session-v4","version":1,"id":"s"}`, ErrForeignFormat},
		{"future version", `{"format":"` + FormatID + `","version":99,"id":"s"}`, ErrUnsupportedVersion},
		{"no version", `{"format":"` + FormatID + `","id":"s"}`, ErrInvalidHeader},
		{"no format", `{"version":1,"id":"s"}`, ErrInvalidHeader},
		{"no id", `{"format":"` + FormatID + `","version":1}`, ErrInvalidHeader},
		{"broken json", `{`, ErrInvalidHeader},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := decodeHeader([]byte(testCase.line))
			if !errors.Is(err, testCase.want) {
				t.Fatalf("want %v, got %v", testCase.want, err)
			}
		})
	}
	if _, err := EncodeHeader("  "); err == nil {
		t.Fatal("a blank session id must be refused")
	}
}

// A record embeds the payload verbatim and detects damage through its checksum.
func TestRecordChecksumDetectsDamage(t *testing.T) {
	payload := []byte(`{"type":"turn/start"}`)
	line, err := EncodeRecord(0, payload)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := decodeRecord(line)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(decoded.Payload) != string(payload) || decoded.Seq != 0 {
		t.Fatalf("unexpected record %+v", decoded)
	}
	damaged := strings.Replace(string(line), "turn/start", "turn/end", 1)
	if _, err := decodeRecord([]byte(damaged)); !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("want ErrCorruptRecord for a changed payload, got %v", err)
	}
	if _, err := decodeRecord([]byte(`{`)); !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("want ErrCorruptRecord for broken json, got %v", err)
	}
	if _, err := EncodeRecord(0, nil); err == nil {
		t.Fatal("an empty payload must be refused")
	}
}

// A session id is an unvalidated string, so the path helper must encode it rather
// than let a separator or a traversal escape the data directory.
func TestSessionPathEncodesTheID(t *testing.T) {
	root := t.TempDir()
	path, err := SessionPath(root, "simple-id_1")
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	if filepath.Dir(path) != root {
		t.Fatalf("the artifact must live directly under the data directory: %s", path)
	}
	if !strings.HasSuffix(path, LogSuffix) {
		t.Fatalf("unexpected suffix: %s", path)
	}
	// A traversal attempt is encoded into a single path element, so it cannot
	// escape the data directory. The reference encodes rather than rejects, and
	// rejecting would refuse ids it stores.
	for _, attempt := range []string{"../escape", "a/b", `a\b`, "..", "."} {
		path, err := SessionPath(root, attempt)
		if err != nil {
			t.Fatalf("session id %q must be encodable: %v", attempt, err)
		}
		if filepath.Dir(path) != root {
			t.Fatalf("session id %q escaped the data directory: %s", attempt, path)
		}
		encoded := strings.TrimSuffix(filepath.Base(path), LogSuffix)
		decoded, err := DecodeID(encoded)
		if err != nil {
			t.Fatalf("decode %q: %v", encoded, err)
		}
		if decoded != attempt {
			t.Fatalf("the encoding must be reversible: %q -> %q", attempt, decoded)
		}
	}
	// A path-unsafe but valid id is encoded, never joined verbatim.
	encoded, err := SessionPath(root, "id:with spaces")
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	if filepath.Dir(encoded) != root {
		t.Fatalf("an encoded id must stay under the root: %s", encoded)
	}
	if strings.Contains(filepath.Base(encoded), " ") || strings.Contains(filepath.Base(encoded), ":") {
		t.Fatalf("the id must be encoded: %s", filepath.Base(encoded))
	}
	if _, err := SessionPath("", "s"); err == nil {
		t.Fatal("a blank data directory must be refused")
	}
	if _, err := SessionPath(root, ""); err == nil {
		t.Fatal("a blank id must be refused")
	}
}

// The lease path is derived from the artifact path so a test can assert that a
// writer released it.
func TestLeasePathDerivation(t *testing.T) {
	root := t.TempDir()
	path, err := SessionPath(root, "s-1")
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	if got := leaseFileFor(root, "s-1"); got != path+LeaseSuffix {
		t.Fatalf("lease path %q does not match artifact %q", got, path)
	}
	if leaseFileFor("", "s") != "" {
		t.Fatal("a blank root must yield no lease path")
	}
}

// EncodeHeaderAtVersion exists for diagnostics and fixtures: a test must be able
// to produce the artifact a newer build would have written.
func TestEncodeHeaderAtVersion(t *testing.T) {
	line, err := EncodeHeaderAtVersion("s-1", 7)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// The header decodes as a header, and the version is refused as unknown.
	if _, err := decodeHeader(line); !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("want ErrUnsupportedVersion for a future version, got %v", err)
	}
	if _, err := EncodeHeaderAtVersion("", 1); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("want ErrInvalidHeader without an id, got %v", err)
	}
	if _, err := EncodeHeaderAtVersion("s-1", 0); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("want ErrInvalidHeader for a non-positive version, got %v", err)
	}
	if _, err := EncodeHeaderAtVersion("s-1", -3); !errors.Is(err, ErrInvalidHeader) {
		t.Fatalf("want ErrInvalidHeader for a negative version, got %v", err)
	}
}
