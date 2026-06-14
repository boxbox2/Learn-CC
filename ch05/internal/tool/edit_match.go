package tool

import (
	"fmt"
	"strings"
)

type EditReplacement struct {
	Content    string
	Normalized bool
}

func ReplaceUnique(content, oldString, newString string) (EditReplacement, error) {
	if oldString == "" {
		return EditReplacement{}, fmt.Errorf("old_string is required")
	}
	if count := strings.Count(content, oldString); count == 1 {
		return EditReplacement{Content: strings.Replace(content, oldString, newString, 1)}, nil
	} else if count > 1 {
		return EditReplacement{}, fmt.Errorf("old_string matched %d times", count)
	}
	newline := detectNewline(content)
	normContent, positions := normalizeNewlinesWithPositions(content)
	normOld, _ := normalizeNewlinesWithPositions(oldString)
	count := strings.Count(normContent, normOld)
	if count == 0 {
		return EditReplacement{}, fmt.Errorf("old_string was not found")
	}
	if count > 1 {
		return EditReplacement{}, fmt.Errorf("old_string matched %d times after newline normalization", count)
	}
	start := strings.Index(normContent, normOld)
	end := start + len(normOld)
	if start < 0 || end >= len(positions) {
		return EditReplacement{}, fmt.Errorf("normalized match could not be mapped to original content")
	}
	originalStart := positions[start]
	originalEnd := positions[end]
	replacement := convertNewlines(newString, newline)
	return EditReplacement{
		Content:    content[:originalStart] + replacement + content[originalEnd:],
		Normalized: true,
	}, nil
}

func detectNewline(content string) string {
	if strings.Count(content, "\r\n") >= strings.Count(content, "\n")-strings.Count(content, "\r\n") && strings.Contains(content, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

func convertNewlines(s, newline string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	if newline == "\r\n" {
		return strings.ReplaceAll(s, "\n", "\r\n")
	}
	return s
}

func normalizeNewlinesWithPositions(s string) (string, []int) {
	var b strings.Builder
	positions := []int{0}
	for i := 0; i < len(s); {
		switch {
		case s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n':
			b.WriteByte('\n')
			i += 2
			positions = append(positions, i)
		case s[i] == '\r':
			b.WriteByte('\n')
			i++
			positions = append(positions, i)
		default:
			b.WriteByte(s[i])
			i++
			positions = append(positions, i)
		}
	}
	return b.String(), positions
}
