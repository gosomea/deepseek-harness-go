package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodeBlocksRejectInvalidAndUnclassifiedPrograms(t *testing.T) {
	valid := "```go runnable=hello\npackage main\nfunc main(){}\n```\n```text output=hello\n```\n"
	for _, tc := range []struct{ name, body, want string }{
		{"valid", valid, ""},
		{"fragment", "```go fragment\nvalue := callerDefined\n```\n", ""},
		{"unclassified", "```go\npackage main\nfunc main(){}\n```\n", "classify Go code"},
		{"syntax", strings.Replace(valid, "func main(){}", "func main( {", 1), "complete package main"},
		{"not command", strings.Replace(valid, "package main", "package library", 1), "package main"},
		{"missing output", strings.Split(valid, "```text")[0], "needs text output"},
		{"orphan output", "```text output=hello\n```\n", "no runnable program"},
		{"duplicate program", valid + valid, "duplicate runnable"},
		{"duplicate output", valid + "```text output=hello\n```\n", "duplicate output"},
		{"invalid id", strings.ReplaceAll(valid, "=hello", "=Hello"), "lowercase"},
		{"unclosed", "```go fragment\nvalue := 1\n", "unclosed code fence"},
		{"tilde fence", strings.ReplaceAll(valid, "```", "~~~"), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := writeFixture(t, root, "guide.md", tc.body)
			err := checkCodeBlocks(root, path, false)
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

func TestCodeBlockOutputTracksRepositoryFile(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "examples/expected.txt", "hello\n")
	for _, tc := range []struct{ name, output, want string }{
		{"matching", "hello", ""},
		{"stale", "goodbye", "differs"},
	} {
		path := writeFixture(t, root, "guide.md", "```text output-file=examples/expected.txt\n"+tc.output+"\n```\n")
		err := checkCodeBlocks(root, path, false)
		if (tc.want == "" && err != nil) || (tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want))) {
			t.Fatalf("%s: %v", tc.name, err)
		}
	}
	for _, name := range []string{"", "../outside.txt", "/outside.txt", "examples\\expected.txt", "missing.txt"} {
		if _, err := localExamplePath(root, name); err == nil {
			t.Fatalf("accepted invalid path %q", name)
		}
	}
}

func TestRunnableExamplesRejectCompileRuntimeAndOutputFailures(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, source, want, failure string }{
		{"valid", "package main\nimport \"fmt\"\nfunc main(){fmt.Println(\"hello\")}", "hello", ""},
		{"compile", "package main\nfunc main(){missing()}", "", "compilation failed"},
		{"runtime", "package main\nfunc main(){panic(\"failed\")}", "", "execution failed"},
		{"output", "package main\nfunc main(){}", "hello", "output mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "guide.md")
			body := "```go runnable=example\n" + tc.source + "\n```\n```text output=example\n"
			if tc.want != "" {
				body += tc.want + "\n"
			}
			body += "```\n"
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			err := checkCodeBlocks(root, path, true)
			if tc.failure == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.failure) {
				t.Fatalf("want %q, got %v", tc.failure, err)
			}
		})
	}
}

func TestRunnableCancellationIsReported(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := runGoExample(ctx, ".", "package main\nfunc main(){}\n"); !errors.Is(err, context.Canceled) {
		t.Fatalf("want canceled context, got %v", err)
	}
}
