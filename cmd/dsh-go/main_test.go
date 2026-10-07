package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gosomea/deepseek-harness-go/cordis"
	"github.com/gosomea/deepseek-harness-go/loader"
)

// writeDocument writes a configuration document into a temporary directory and
// returns its path, so each test exercises the file-reading path the command uses.
func writeDocument(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cordis.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestCheckReportsEveryEntryByOutcome pins the check output shape and order.
func TestCheckReportsEveryEntryByOutcome(t *testing.T) {
	path := writeDocument(t, `{"entries":[{"id":"b","name":"leaf"},{"id":"a","name":"leaf"},{"id":"off","name":"leaf","disabled":true}]}`)
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-config", path, "-plugin", "leaf"}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v (stderr %s)", err, stderr.String())
	}
	got := stdout.String()
	if !strings.HasPrefix(got, "active a\nactive b\n") {
		t.Fatalf("active entries should be sorted:\n%s", got)
	}
	if !strings.Contains(got, "unmounted off disabled") {
		t.Fatalf("a disabled entry must be reported:\n%s", got)
	}
	if !strings.Contains(got, "summary active=2 pending=0 unmounted=1") {
		t.Fatalf("summary line:\n%s", got)
	}
}

// TestCheckIsReproducible keeps two runs of the same document identical.
func TestCheckIsReproducible(t *testing.T) {
	path := writeDocument(t, `{"entries":[{"id":"x","name":"leaf"},{"id":"y","name":"leaf"}]}`)
	var first, second bytes.Buffer
	if err := run([]string{"-config", path, "-plugin", "leaf"}, &first, &first); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-config", path, "-plugin", "leaf"}, &second, &second); err != nil {
		t.Fatal(err)
	}
	if first.String() != second.String() {
		t.Fatalf("output is not reproducible:\n%s\n%s", first.String(), second.String())
	}
}

// TestDumpPrintsTheDeclaredShape pins that dump renders loader's declared format.
func TestDumpPrintsTheDeclaredShape(t *testing.T) {
	path := writeDocument(t, `{"entries":[{"id":"g","group":true,"config":[{"id":"kid","name":"leaf"}]}]}`)
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-config", path, "-plugin", "leaf", "-mode", "dump"}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v", err)
	}
	got := stdout.String()
	if !strings.Contains(got, "g [group]") || !strings.Contains(got, "  g:kid [plugin]") {
		t.Fatalf("dump output:\n%s", got)
	}
	if strings.Contains(got, "state=") {
		t.Fatalf("the declared dump must not report runtime state:\n%s", got)
	}
}

// TestUsageErrorsAreDistinct pins the rejected command lines and their exit class.
func TestUsageErrorsAreDistinct(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"missing config", []string{}},
		{"unknown mode", []string{"-config", "x.json", "-mode", "bogus"}},
		{"unknown flag", []string{"-nope"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(tc.args, &stdout, &stderr)
			if err == nil {
				t.Fatal("expected a usage error")
			}
			var usage usageError
			if !errors.As(err, &usage) {
				t.Fatalf("error %v is not a usage error", err)
			}
		})
	}
}

// TestMissingDocumentIsAUsageError keeps an unreadable input out of the
// configuration-error class.
func TestMissingDocumentIsAUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"-config", filepath.Join(t.TempDir(), "absent.json")}, &stdout, &stderr)
	var usage usageError
	if !errors.As(err, &usage) {
		t.Fatalf("error %v should be a usage error", err)
	}
}

// TestUnknownPluginAndMalformedDocumentFailWithoutMounting pins the two failure
// classes the CLI reports with a non-usage exit.
func TestUnknownPluginAndMalformedDocumentFailWithoutMounting(t *testing.T) {
	path := writeDocument(t, `{"entries":[{"id":"a","name":"missing"}]}`)
	var stdout, stderr bytes.Buffer
	err := run([]string{"-config", path, "-plugin", "leaf"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("an unregistered plugin must fail")
	}
	path = writeDocument(t, `{"entries":[{"id":"a","name":"leaf","nope":1}]}`)
	stdout.Reset()
	err = run([]string{"-config", path, "-plugin", "leaf"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("a malformed document must fail")
	}
	if stdout.Len() != 0 {
		t.Fatalf("a rejected document must print nothing: %s", stdout.String())
	}
}

// TestPluginFlagRegistersEachName covers spaced registrations in one flag value.
func TestPluginFlagRegistersEachName(t *testing.T) {
	path := writeDocument(t, `{"entries":[{"id":"a","name":"one"},{"id":"b","name":"two"}]}`)
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-config", path, "-plugin", " one , two "}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stdout.String(), "summary active=2") {
		t.Fatalf("output:\n%s", stdout.String())
	}
}

// TestDuplicatePluginNameIsRejected keeps a repeated registration loud.
func TestDuplicatePluginNameIsRejected(t *testing.T) {
	path := writeDocument(t, `{"entries":[{"id":"a","name":"one"}]}`)
	var stdout, stderr bytes.Buffer
	err := run([]string{"-config", path, "-plugin", "one,one"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("a duplicate registration must be rejected")
	}
	var usage usageError
	if !errors.As(err, &usage) {
		t.Fatalf("error %v should be a usage error: a name listed twice is a command-line mistake", err)
	}
	if !strings.Contains(err.Error(), "twice") {
		t.Fatalf("error %v should say the name is repeated", err)
	}
}

// TestAllActiveEntriesSummarize keeps the healthy path's summary stable.
func TestAllActiveEntriesSummarize(t *testing.T) {
	path := writeDocument(t, `{"entries":[{"id":"a","name":"leaf"}]}`)
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-config", path, "-plugin", "leaf"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "summary active=1 pending=0 unmounted=0") {
		t.Fatalf("output:\n%s", stdout.String())
	}
}

// TestCheckReleasesEveryMountedInstance pins the resource contract directly: the
// command's check path mounts plugins and must release every one of them before
// returning. A counting plugin makes that observable instead of inferred.
func TestCheckReleasesEveryMountedInstance(t *testing.T) {
	var live int64
	catalog := loader.NewCatalog()
	if err := catalog.Register("counter", func() cordis.Plugin {
		return cordis.Plugin{
			Name: "counter",
			Apply: func(*cordis.Context, any) (cordis.Cleanup, error) {
				atomic.AddInt64(&live, 1)
				return func() error { atomic.AddInt64(&live, -1); return nil }, nil
			},
		}
	}); err != nil {
		t.Fatal(err)
	}
	document := []byte(`{"entries":[{"id":"a","name":"counter"},{"id":"b","name":"counter"}]}`)
	var stdout bytes.Buffer
	if err := check(document, catalog, &stdout); err != nil {
		t.Fatalf("check: %v", err)
	}
	if got := atomic.LoadInt64(&live); got != 0 {
		t.Fatalf("live instances after check = %d, want 0: the command must release every mount", got)
	}
	if !strings.Contains(stdout.String(), "summary active=2") {
		t.Fatalf("output:\n%s", stdout.String())
	}
}

// TestLoadTreeReleasesOnMountFailure keeps a failed mount from leaking the tree it
// created: an unknown plugin name must leave no instance behind.
func TestLoadTreeReleasesOnMountFailure(t *testing.T) {
	var live int64
	catalog := loader.NewCatalog()
	if err := catalog.Register("ok", func() cordis.Plugin {
		return cordis.Plugin{
			Name: "ok",
			Apply: func(*cordis.Context, any) (cordis.Cleanup, error) {
				atomic.AddInt64(&live, 1)
				return func() error { atomic.AddInt64(&live, -1); return nil }, nil
			},
		}
	}); err != nil {
		t.Fatal(err)
	}
	// The second entry names an unregistered plugin, so mounting fails after the
	// first entry already acquired its resource.
	document := []byte(`{"entries":[{"id":"a","name":"ok"},{"id":"b","name":"missing"}]}`)
	if _, err := loadTree(document, catalog); err == nil {
		t.Fatal("mounting an unknown plugin must fail")
	}
	if got := atomic.LoadInt64(&live); got != 0 {
		t.Fatalf("live instances after a failed mount = %d, want 0", got)
	}
}

// TestSplitListIgnoresEmptyEntries keeps a trailing comma harmless.
func TestSplitListIgnoresEmptyEntries(t *testing.T) {
	if got := splitList("a,,b,"); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("splitList = %v", got)
	}
	if got := splitList("   "); got != nil {
		t.Fatalf("splitList = %v, want nil", got)
	}
}
