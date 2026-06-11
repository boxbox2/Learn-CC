package tool

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type Limits struct {
	CommandTimeout time.Duration
	MaxResultBytes int
	MaxStdoutBytes int
	MaxStderrBytes int
	MaxFileBytes   int
	MaxMatches     int
}

type LimitedText struct {
	Text          string
	Truncated     bool
	OriginalBytes int
	ReturnedBytes int
}

func DefaultLimits() Limits {
	return Limits{
		CommandTimeout: 30 * time.Second,
		MaxResultBytes: 40 * 1024,
		MaxStdoutBytes: 24 * 1024,
		MaxStderrBytes: 12 * 1024,
		MaxFileBytes:   64 * 1024,
		MaxMatches:     100,
	}
}

func (l Limits) withDefaults() Limits {
	def := DefaultLimits()
	if l.CommandTimeout <= 0 {
		l.CommandTimeout = def.CommandTimeout
	}
	if l.MaxResultBytes <= 0 {
		l.MaxResultBytes = def.MaxResultBytes
	}
	if l.MaxStdoutBytes <= 0 {
		l.MaxStdoutBytes = def.MaxStdoutBytes
	}
	if l.MaxStderrBytes <= 0 {
		l.MaxStderrBytes = def.MaxStderrBytes
	}
	if l.MaxFileBytes <= 0 {
		l.MaxFileBytes = def.MaxFileBytes
	}
	if l.MaxMatches <= 0 {
		l.MaxMatches = def.MaxMatches
	}
	return l
}

func LimitText(s string, maxBytes int) LimitedText {
	original := len(s)
	if maxBytes <= 0 || original <= maxBytes {
		return LimitedText{Text: s, OriginalBytes: original, ReturnedBytes: original}
	}
	marker := fmt.Sprintf("\n\n[... truncated: original=%d bytes, limit=%d bytes ...]\n\n", original, maxBytes)
	if len(marker) >= maxBytes {
		text := safePrefix(s, maxBytes)
		return LimitedText{Text: text, Truncated: true, OriginalBytes: original, ReturnedBytes: len(text)}
	}
	budget := maxBytes - len(marker)
	headBudget := budget / 2
	tailBudget := budget - headBudget
	text := safePrefix(s, headBudget) + marker + safeSuffix(s, tailBudget)
	return LimitedText{Text: text, Truncated: true, OriginalBytes: original, ReturnedBytes: len(text)}
}

func safePrefix(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	end := maxBytes
	for end > 0 && !utf8.ValidString(s[:end]) {
		end--
	}
	return s[:end]
}

func safeSuffix(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	start := len(s) - maxBytes
	for start < len(s) && !utf8.ValidString(s[start:]) {
		start++
	}
	return strings.TrimPrefix(s[start:], "\n")
}
