package logs

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"
)

func TestRingBuffer_WriteAndReadAll(t *testing.T) {
	rb := NewRingBuffer(10) // Small capacity to test wrap-around

	n, err := rb.Write([]byte("hello"))
	if err != nil || n != 5 {
		t.Fatalf("Write failed: n=%d, err=%v", n, err)
	}

	if string(rb.ReadAll()) != "hello" {
		t.Errorf("Expected 'hello', got '%s'", rb.ReadAll())
	}

	// Write more to trigger wrap-around
	n, err = rb.Write([]byte(" world!")) // total length 12 > capacity 10
	if err != nil || n != 7 {
		t.Fatalf("Write failed: n=%d, err=%v", n, err)
	}

	// The buffer should contain the last 10 bytes: "llo world!"
	expected := "llo world!"
	if got := string(rb.ReadAll()); got != expected {
		t.Errorf("Expected '%s', got '%s'", expected, got)
	}
}

func TestRingBuffer_TailLines(t *testing.T) {
	rb := NewRingBuffer(100)
	_, _ = rb.Write([]byte("line1\nline2\nline3\n"))

	tests := []struct {
		count    int
		expected string
	}{
		{0, "line1\nline2\nline3\n"},
		{1, "line3\n"},
		{2, "line2\nline3\n"},
		{3, "line1\nline2\nline3\n"},
		{4, "line1\nline2\nline3\n"},
	}

	for _, tt := range tests {
		got := string(rb.TailLines(tt.count))
		if got != tt.expected {
			t.Errorf("TailLines(%d) = %q, expected %q", tt.count, got, tt.expected)
		}
	}
}

func TestRingBuffer_Follow(t *testing.T) {
	rb := NewRingBuffer(100)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := rb.Follow(ctx)

	var wg sync.WaitGroup
	wg.Add(1)

	var received bytes.Buffer
	go func() {
		defer wg.Done()
		for chunk := range ch {
			received.Write(chunk)
		}
	}()

	_, _ = rb.Write([]byte("hello"))
	time.Sleep(10 * time.Millisecond)
	_, _ = rb.Write([]byte(" world"))
	time.Sleep(10 * time.Millisecond)

	cancel()
	wg.Wait()

	if received.String() != "hello world" {
		t.Errorf("Expected 'hello world', got %q", received.String())
	}
}
