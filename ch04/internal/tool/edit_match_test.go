package tool

import (
	"strings"
	"testing"
)

func TestReplaceUniqueExact(t *testing.T) {
	got, err := ReplaceUnique("hello world", "world", "mew")
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "hello mew" || got.Normalized {
		t.Fatalf("unexpected replacement: %+v", got)
	}
}

func TestReplaceUniqueCRLFFallback(t *testing.T) {
	content := "func main() {\r\n\tprintln(\"old\")\r\n}\r\n"
	old := "func main() {\n\tprintln(\"old\")\n}"
	newText := "func main() {\n\tprintln(\"new\")\n}"
	got, err := ReplaceUnique(content, old, newText)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Normalized {
		t.Fatal("expected normalized match")
	}
	if !strings.Contains(got.Content, "\r\n\tprintln(\"new\")\r\n") {
		t.Fatalf("replacement did not preserve CRLF: %q", got.Content)
	}
}

func TestReplaceUniqueNoMatchAndMultiple(t *testing.T) {
	if _, err := ReplaceUnique("abc", "zzz", "x"); err == nil {
		t.Fatal("expected no match error")
	}
	if _, err := ReplaceUnique("abc abc", "abc", "x"); err == nil {
		t.Fatal("expected multiple match error")
	}
}
