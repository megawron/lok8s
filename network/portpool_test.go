package network

import "testing"

func TestPortPool(t *testing.T) {
	p := NewPortPool(30000, 30002)

	// Allocate first
	p1, err := p.Allocate("id1")
	if err != nil || p1 != 30000 {
		t.Errorf("expected 30000, got %d, err: %v", p1, err)
	}

	// Double allocate same ID should return same port
	p1Again, err := p.Allocate("id1")
	if err != nil || p1Again != 30000 {
		t.Errorf("expected 30000 again, got %d, err: %v", p1Again, err)
	}

	// Allocate second
	p2, err := p.Allocate("id2")
	if err != nil || p2 != 30001 {
		t.Errorf("expected 30001, got %d, err: %v", p2, err)
	}

	// Allocate third
	p3, err := p.Allocate("id3")
	if err != nil || p3 != 30002 {
		t.Errorf("expected 30002, got %d, err: %v", p3, err)
	}

	// Allocate fourth should fail
	_, err = p.Allocate("id4")
	if err == nil {
		t.Error("expected allocation to fail, but it succeeded")
	}

	// Release and re-allocate
	p.Release("id2")
	p2New, err := p.Allocate("id4")
	if err != nil || p2New != 30001 {
		t.Errorf("expected 30001 after release, got %d, err: %v", p2New, err)
	}
}
