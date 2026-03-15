package logs

import (
	"bytes"
	"testing"
)

func TestPrefixWriter(t *testing.T) {
	var buf bytes.Buffer
	pw := NewPrefixWriter(&buf, "[pod1] ")

	n, err := pw.Write([]byte("hello\nworld\n"))
	if err != nil || n != 12 {
		t.Fatalf("Write failed: n=%d, err=%v", n, err)
	}

	expected := "[pod1] hello\n[pod1] world\n"
	if got := buf.String(); got != expected {
		t.Errorf("Expected %q, got %q", expected, got)
	}

	buf.Reset()
	// Write without trailing newline, then write rest
	_, _ = pw.Write([]byte("part1"))
	_, _ = pw.Write([]byte(" part2\nnew line"))

	expected2 := "[pod1] part1 part2\n[pod1] new line"
	if got := buf.String(); got != expected2 {
		t.Errorf("Expected %q, got %q", expected2, got)
	}
}
