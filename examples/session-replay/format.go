package main

import (
	"strings"
)

// splitLines splits a text body into lines without a trailing empty element.
func splitLines(body string) []string {
	return strings.Split(strings.TrimRight(body, "\n"), "\n")
}

// joinLines rejoins lines with a terminating newline.
func joinLines(lines []string) string {
	return strings.Join(lines, "\n") + "\n"
}

// replaceOnce replaces the first occurrence of old with new.
func replaceOnce(text, old, replacement string) string {
	index := strings.Index(text, old)
	if index < 0 {
		return text
	}
	return text[:index] + replacement + text[index+len(old):]
}
