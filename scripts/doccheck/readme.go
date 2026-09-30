package main

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

var readmeSections = map[string][]string{
	"project-overview":    {"概述", "目录", "快速开始", "学习路径", "开发与验证", "授权"},
	"documentation-index": {"概述", "目录", "学习路径", "查阅参考", "开发与验收"},
	"package-library":     {"概述", "目录", "职责", "使用", "理解实现", "进一步阅读", "验证", "限制"},
	"command-example":     {"概述", "目录", "职责", "使用", "验证", "限制"},
	"command-tool":        {"概述", "目录", "职责", "使用", "验证", "限制"},
}

// Metadata uses a deliberately small YAML subset: two double-quoted strings.
// README kinds select templates; they do not describe the plugin runtime type.
func readmeMetadata(body string) (map[string]string, error) {
	lines := strings.Split(body, "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return nil, errors.New("needs description and kind frontmatter; see docs/documentation.md")
	}
	meta := make(map[string]string)
	closed := false
	for _, line := range lines[1:] {
		if line == "---" {
			closed = true
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok || (key != "description" && key != "kind") {
			return nil, fmt.Errorf("unsupported metadata line %q; use description and kind", line)
		}
		if _, duplicate := meta[key]; duplicate {
			return nil, fmt.Errorf("duplicate metadata field %s", key)
		}
		value = strings.TrimSpace(value)
		if !strings.HasPrefix(value, "\"") {
			return nil, fmt.Errorf("%s needs a non-empty double-quoted string", key)
		}
		value, err := strconv.Unquote(value)
		if err != nil || strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\r\n") {
			return nil, fmt.Errorf("%s needs a non-empty double-quoted string", key)
		}
		meta[key] = value
	}
	if !closed || meta["description"] == "" || meta["kind"] == "" {
		return nil, errors.New("needs closed description and kind frontmatter")
	}
	return meta, nil
}

func expectedReadmeKind(root, path string) string {
	rel, _ := filepath.Rel(root, path)
	switch filepath.ToSlash(rel) {
	case "README.md":
		return "project-overview"
	case "docs/README.md":
		return "documentation-index"
	}
	paths, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "*.go"))
	for _, source := range paths {
		if strings.HasSuffix(source, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), source, nil, parser.PackageClauseOnly)
		if err == nil && file.Name.Name == "main" {
			if strings.HasPrefix(filepath.ToSlash(rel), "examples/") {
				return "command-example"
			}
			return "command-tool"
		}
	}
	return "package-library"
}

var htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)

func checkREADME(root, path string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	meta, err := readmeMetadata(string(body))
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	wantKind := expectedReadmeKind(root, path)
	if meta["kind"] != wantKind {
		return fmt.Errorf("%s: kind must be %s for this entry; see docs/documentation.md", path, wantKind)
	}
	sections := make(map[string]string)
	var order []string
	var current string
	for _, line := range strings.Split(stripFences(htmlComment.ReplaceAllString(string(body), "")), "\n") {
		if strings.HasPrefix(line, "## ") {
			current = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			if _, duplicate := sections[current]; duplicate {
				return fmt.Errorf("%s: duplicate section %s", path, current)
			}
			sections[current] = ""
			order = append(order, current)
		} else if current != "" {
			sections[current] += line + "\n"
		}
	}
	var errs []error
	previous := -1
	for _, heading := range readmeSections[wantKind] {
		content, ok := sections[heading]
		if !ok {
			errs = append(errs, fmt.Errorf("%s: missing section %s", path, heading))
			continue
		}
		position := 0
		for order[position] != heading {
			position++
		}
		if position < previous {
			errs = append(errs, fmt.Errorf("%s: section %s is out of template order", path, heading))
		}
		previous = position
		content = strings.TrimSpace(content)
		meaningful := strings.ContainsFunc(content, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) })
		if !meaningful || content == "TODO" || content == "待补充" {
			errs = append(errs, fmt.Errorf("%s: section %s needs explanatory content", path, heading))
		}
	}
	return errors.Join(errs...)
}
