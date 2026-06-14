package tool

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLimitTextUnderLimit(t *testing.T) {
	got := LimitText("hello", 10)
	if got.Text != "hello" || got.Truncated {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestLimitTextTruncatesWithMetadata(t *testing.T) {
	input := strings.Repeat("a", 100) + "TAIL"
	got := LimitText(input, 80)
	if !got.Truncated {
		t.Fatal("expected truncated")
	}
	if got.OriginalBytes != len(input) {
		t.Fatalf("original bytes = %d", got.OriginalBytes)
	}
	if got.ReturnedBytes != len(got.Text) {
		t.Fatalf("returned bytes = %d len = %d", got.ReturnedBytes, len(got.Text))
	}
	if !strings.Contains(got.Text, "truncated") || !strings.HasSuffix(got.Text, "TAIL") {
		t.Fatalf("bad truncated text: %q", got.Text)
	}
}

func TestLimitTextDoesNotSplitUTF8(t *testing.T) {
	input := strings.Repeat("界", 30)
	got := LimitText(input, 50)
	if !utf8.ValidString(got.Text) {
		t.Fatalf("invalid utf8: %q", got.Text)
	}
}
