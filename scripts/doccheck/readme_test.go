package main

import (
	"strings"
	"testing"
)

func fixtureREADME(kind string) string {
	var body strings.Builder
	body.WriteString("---\ndescription: \"A fixture with executable documentation.\"\nkind: \"" + kind + "\"\n---\n\n# Fixture\n")
	for _, heading := range readmeSections[kind] {
		body.WriteString("\n## " + heading + "\n\nDocumented behavior.\n")
	}
	return body.String()
}

func TestREADMERejectsMissingMetadataAndEmptySections(t *testing.T) {
	valid := fixtureREADME("package-library")
	for _, tc := range []struct {
		name, body, want string
	}{
		{"valid", valid, ""},
		{"no metadata", "# Library\n", "frontmatter"},
		{"missing description", strings.Replace(valid, "description: \"A fixture with executable documentation.\"\n", "", 1), "frontmatter"},
		{"empty description", strings.Replace(valid, "A fixture with executable documentation.", "", 1), "non-empty"},
		{"wrong kind", strings.Replace(valid, "kind: \"package-library\"", "kind: \"command-tool\"", 1), "kind must be"},
		{"empty use", strings.Replace(valid, "## 使用\n\nDocumented behavior.", "## 使用\n", 1), "使用 needs"},
		{"comment only", strings.Replace(valid, "## 使用\n\nDocumented behavior.", "## 使用\n\n<!-- TODO explain usage -->", 1), "使用 needs"},
		{"placeholder", strings.Replace(valid, "## 使用\n\nDocumented behavior.", "## 使用\n\nTODO", 1), "使用 needs"},
		{"missing limit", strings.Replace(valid, "## 限制", "## Other", 1), "missing section 限制"},
		{"duplicate", valid + "\n## 使用\n\nAnother use.\n", "duplicate section"},
		{"order", strings.Replace(strings.Replace(valid, "## 概述", "## 目录", 1), "## 目录\n\nDocumented behavior.\n\n## 目录", "## 目录\n\nDocumented behavior.\n\n## 概述", 1), "out of template order"},
		{"fake section in fence", strings.Replace(valid, "## 使用\n\nDocumented behavior.", "```text\n## 使用\nDocumented behavior.\n```", 1), "missing section 使用"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := writeFixture(t, root, "library/README.md", tc.body)
			err := checkREADME(root, path)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

func TestREADMEKindsFollowRepositoryEntry(t *testing.T) {
	for _, tc := range []struct{ path, kind, source string }{
		{"README.md", "project-overview", ""},
		{"docs/README.md", "documentation-index", ""},
		{"examples/sample/README.md", "command-example", "package main\n"},
		{"scripts/sample/README.md", "command-tool", "package main\n"},
		{"library/README.md", "package-library", "package library\n"},
	} {
		root := t.TempDir()
		path := writeFixture(t, root, tc.path, fixtureREADME(tc.kind))
		if tc.source != "" {
			writeFixture(t, root, strings.TrimSuffix(tc.path, "README.md")+"main.go", tc.source)
		}
		if err := checkREADME(root, path); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMetadataRejectsDuplicatesAndMalformedValues(t *testing.T) {
	for _, body := range []string{
		"---\ndescription: \"ok\"\nkind: \"package-library\"\nkind: \"command-tool\"\n---\n",
		"---\ndescription: unquoted\nkind: \"package-library\"\n---\n",
		"---\ndescription: \"ok\"\nkind: \"package-library\"\n",
		"---\ndescription: \"ok\"\nkind: \"package-library\"\ntags: []\n---\n",
	} {
		if _, err := readmeMetadata(body); err == nil {
			t.Fatalf("accepted malformed metadata: %s", body)
		}
	}
}
