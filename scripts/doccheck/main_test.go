package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixture(t *testing.T, root, name, body string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExportGateRejectsMissingComments(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		body    string
		missing bool
	}{
		{"package example\nfunc Public(){}\n", true},
		{"package example\n// Public has documentation.\nfunc Public(){}\n", false},
		{"package example\n// Public is a record.\ntype Public struct { Field string }\n", true},
		{"package example\n// Public is a record.\ntype Public struct {\n// Field is documented.\nField string\n}\n", false},
		{"package example\nvar Public = 1\n", true},
	} {
		path := writeFixture(t, root, "source.go", tc.body)
		if (checkExports(path) != nil) != tc.missing {
			t.Fatal(tc.body)
		}
	}
}

func TestLinkGateRejectsMissingTargetsAndFragments(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "target.md", "# 中文 标题\n")
	for _, tc := range []struct {
		link string
		bad  bool
	}{
		{"[ok](target.md#中文-标题)", false}, {"[bad](missing.md)", true},
		{"[bad](target.md#missing)", true}, {"[bad](../outside.md)", true},
		{"[web](https://example.com)", false}, {"```md\n[example](missing.md)\n```", false},
		{"`NewKey[T](\"model\")`", false},
	} {
		path := writeFixture(t, root, "README.md", tc.link)
		if (checkLinks(root, path) != nil) != tc.bad {
			t.Fatal(tc.link)
		}
	}
}

func TestDocumentationGateDetectsREADMEAndAPIDrift(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "cordis/doc.go", "// Package cordis is a fixture.\npackage cordis\n// Public is documented.\nfunc Public(){}\n")
	api, err := renderAPI(root)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, "docs/cordis/api.md", string(api))
	writeFixture(t, root, "docs/cordis/lifecycle.md", "# Lifecycle\n")
	writeFixture(t, root, "docs/cordis/services.md", "# Services\n")
	writeFixture(t, root, "docs/cordis/events.md", "# Events\n")
	writeFixture(t, root, "cordis/fiber.go", "package cordis\n")
	writeFixture(t, root, "cordis/plugin.go", "package cordis\n")
	if err := checkDocs(root); err == nil || !strings.Contains(err.Error(), "needs README") {
		t.Fatal(err)
	}
	writeFixture(t, root, "cordis/README.md", fixtureREADME("package-library"))
	if err := checkDocs(root); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, "docs/cordis/api.md", "stale\n")
	if err := checkDocs(root); err == nil || !strings.Contains(err.Error(), "API stale") {
		t.Fatal(err)
	}
}

func TestCoverageGateRejectsEmptyAndBelowThresholdProfiles(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		profile string
		bad     bool
	}{
		{"mode: atomic\n", true},
		{"mode: atomic\ngithub.com/gosomea/deepseek-harness-go/cordis/a.go:1.1,2.1 9 1\ngithub.com/gosomea/deepseek-harness-go/cordis/a.go:3.1,4.1 1 0\n", false},
		{"mode: atomic\ngithub.com/gosomea/deepseek-harness-go/cordis/a.go:1.1,2.1 8 1\ngithub.com/gosomea/deepseek-harness-go/cordis/a.go:3.1,4.1 2 0\n", true},
	} {
		path := writeFixture(t, root, "coverage.out", tc.profile)
		if (checkCoverage(path) != nil) != tc.bad {
			t.Fatal(tc.profile)
		}
	}
}
