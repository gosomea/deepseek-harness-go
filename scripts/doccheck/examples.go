package main

import (
	"context"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type codeBlock struct {
	info string
	body string
	line int
	end  int
}

func codeBlocks(body string) ([]codeBlock, error) {
	var blocks []codeBlock
	var fence, info string
	var content strings.Builder
	start := 0
	for index, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if len(line)-len(trimmed) > 3 {
			trimmed = line
		}
		if fence != "" {
			closing := strings.TrimSpace(trimmed)
			if len(closing) >= len(fence) && strings.Trim(closing, fence[:1]) == "" {
				blocks = append(blocks, codeBlock{info, content.String(), start, index + 1})
				fence = ""
				content.Reset()
			} else {
				content.WriteString(line + "\n")
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			char := trimmed[:1]
			length := len(trimmed) - len(strings.TrimLeft(trimmed, char))
			fence, info, start = trimmed[:length], strings.Join(strings.Fields(trimmed[length:]), " "), index+1
		}
	}
	if fence != "" {
		return nil, fmt.Errorf("line %d: unclosed code fence", start)
	}
	return blocks, nil
}

var exampleID = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func checkCodeBlocks(root, path string, execute bool) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	blocks, err := codeBlocks(string(body))
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	programs, outputs := map[string]codeBlock{}, map[string]codeBlock{}
	var errs []error
	for _, block := range blocks {
		fields := strings.Fields(block.info)
		if len(fields) == 0 {
			continue
		}
		fail := func(message string) { errs = append(errs, fmt.Errorf("%s:%d: %s", path, block.line, message)) }
		if fields[0] == "go" {
			if len(fields) == 2 && fields[1] == "fragment" {
				continue
			}
			if len(fields) != 2 || !strings.HasPrefix(fields[1], "runnable=") {
				rel, _ := filepath.Rel(root, path)
				if filepath.ToSlash(rel) == "docs/cordis/api.md" && len(fields) == 1 {
					continue // Only this freshness-gated generator owns declaration blocks.
				}
				fail("classify Go code as go runnable=<id> or go fragment; see docs/documentation.md")
				continue
			}
			id := strings.TrimPrefix(fields[1], "runnable=")
			if !exampleID.MatchString(id) {
				fail("runnable id must use lowercase letters, digits and hyphens")
				continue
			}
			if _, duplicate := programs[id]; duplicate {
				fail("duplicate runnable id " + id)
				continue
			}
			file, parseErr := parser.ParseFile(token.NewFileSet(), path, block.body, parser.AllErrors)
			if parseErr != nil || file.Name.Name != "main" {
				fail(fmt.Sprintf("runnable %s needs a complete package main: %v", id, parseErr))
			}
			programs[id] = block
		} else if fields[0] == "text" && len(fields) == 2 {
			if id, ok := strings.CutPrefix(fields[1], "output="); ok {
				if !exampleID.MatchString(id) {
					fail("invalid output id " + id)
				} else if _, duplicate := outputs[id]; duplicate {
					fail("duplicate output id " + id)
				} else {
					outputs[id] = block
				}
			} else if name, ok := strings.CutPrefix(fields[1], "output-file="); ok {
				dest, localErr := localExamplePath(root, name)
				if localErr != nil {
					fail(localErr.Error())
				} else if want, readErr := os.ReadFile(dest); readErr != nil || string(want) != block.body {
					fail("output differs from repository file " + name)
				}
			}
		}
	}
	for id := range outputs {
		if _, ok := programs[id]; !ok {
			errs = append(errs, fmt.Errorf("%s: output %s has no runnable program", path, id))
		}
	}
	// Preserve document order so programs and diagnostics have stable execution order.
	for _, block := range blocks {
		id, ok := strings.CutPrefix(block.info, "go runnable=")
		if !ok {
			continue
		}
		want, ok := outputs[id]
		if !ok {
			errs = append(errs, fmt.Errorf("%s:%d: runnable %s needs text output=%s", path, block.line, id, id))
			continue
		}
		if execute && len(errs) == 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			got, runErr := runGoExample(ctx, root, block.body)
			cancel()
			if runErr != nil {
				errs = append(errs, fmt.Errorf("%s:%d: runnable %s: %w", path, block.line, id, runErr))
			} else if got != want.body {
				errs = append(errs, fmt.Errorf("%s:%d: runnable %s output mismatch\nwant:\n%s\ngot:\n%s", path, block.line, id, want.body, got))
			}
		}
	}
	return errors.Join(errs...)
}

func localExamplePath(root, name string) (string, error) {
	if name == "" || filepath.IsAbs(name) || strings.Contains(name, "\\") {
		return "", fmt.Errorf("output-file needs a repository-relative path: %s", name)
	}
	path := filepath.Join(root, filepath.FromSlash(name))
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("output-file escapes repository: %s", name)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absRoot, err = filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", err
	}
	absResolved, err := filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	rel, err = filepath.Rel(absRoot, absResolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("output-file symlink escapes repository: %s", name)
	}
	return resolved, nil
}

func runGoExample(ctx context.Context, root, source string) (string, error) {
	dir, err := os.MkdirTemp("", "cordis-doc-example-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		return "", err
	}
	// Build first so cancellation can stop the example process itself, instead
	// of only the go-run parent while its child retains output pipes.
	binary := filepath.Join(dir, "example.exe")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, path)
	build.Dir, build.WaitDelay = root, time.Second
	output, err := build.CombinedOutput()
	if ctx.Err() != nil {
		return "", fmt.Errorf("Go example stopped: %w", ctx.Err())
	}
	if err != nil {
		return "", fmt.Errorf("Go example compilation failed: %w\n%s", err, output)
	}
	cmd := exec.CommandContext(ctx, binary)
	cmd.Dir, cmd.WaitDelay = root, time.Second
	output, err = cmd.Output()
	if ctx.Err() != nil {
		return "", fmt.Errorf("Go example stopped: %w", ctx.Err())
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return "", fmt.Errorf("Go example execution failed: %w\n%s", err, exit.Stderr)
		}
		return "", fmt.Errorf("Go example execution failed: %w", err)
	}
	return string(output), nil
}

func checkExamples(root string) error {
	var errs []error
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "bin" || d.Name() == "validation") {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(path, ".md") {
			if err := checkCodeBlocks(root, path, true); err != nil {
				errs = append(errs, err)
			}
		}
		return nil
	})
	return errors.Join(append(errs, err)...)
}
