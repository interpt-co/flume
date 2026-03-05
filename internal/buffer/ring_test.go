package buffer

import (
	"sync"
	"testing"
)

func TestRing_PushAndGetAll(t *testing.T) {
	r := NewRing[int](5)
	r.Push(1)
	r.Push(2)
	r.Push(3)

	got := r.GetAll()
	want := []int{1, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("Len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("GetAll()[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestRing_Overflow(t *testing.T) {
	r := NewRing[int](3)
	for i := 1; i <= 5; i++ {
		r.Push(i)
	}

	// Oldest two (1, 2) should be overwritten.
	got := r.GetAll()
	want := []int{3, 4, 5}
	if len(got) != len(want) {
		t.Fatalf("Len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("GetAll()[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestRing_OverflowMultipleWraps(t *testing.T) {
	r := NewRing[int](3)
	for i := 1; i <= 10; i++ {
		r.Push(i)
	}

	got := r.GetAll()
	want := []int{8, 9, 10}
	if len(got) != len(want) {
		t.Fatalf("Len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("GetAll()[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestRing_GetRange(t *testing.T) {
	r := NewRing[string](5)
	r.Push("a")
	r.Push("b")
	r.Push("c")
	r.Push("d")
	r.Push("e")

	got := r.GetRange(1, 3)
	want := []string{"b", "c", "d"}
	if len(got) != len(want) {
		t.Fatalf("Len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("GetRange()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRing_GetRange_AfterOverflow(t *testing.T) {
	r := NewRing[int](3)
	for i := 1; i <= 5; i++ {
		r.Push(i)
	}
	// Buffer: [3, 4, 5]
	got := r.GetRange(0, 2)
	want := []int{3, 4}
	if len(got) != len(want) {
		t.Fatalf("Len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("GetRange()[%d] = %d, want %d", i, got[i], want[i])
		}
	}
}

func TestRing_GetRange_OutOfBounds(t *testing.T) {
	r := NewRing[int](5)
	r.Push(1)
	r.Push(2)

	// Start beyond length.
	got := r.GetRange(10, 5)
	if len(got) != 0 {
		t.Errorf("expected empty slice for out-of-range start, got %v", got)
	}

	// Count exceeds available items.
	got = r.GetRange(1, 100)
	if len(got) != 1 {
		t.Errorf("expected 1 item, got %d", len(got))
	}
	if got[0] != 2 {
		t.Errorf("got %d, want 2", got[0])
	}
}

func TestRing_GetRange_NegativeStart(t *testing.T) {
	r := NewRing[int](5)
	r.Push(10)
	r.Push(20)

	got := r.GetRange(-5, 2)
	if len(got) != 2 {
		t.Fatalf("Len = %d, want 2", len(got))
	}
	if got[0] != 10 || got[1] != 20 {
		t.Errorf("got %v, want [10, 20]", got)
	}
}

func TestRing_Len(t *testing.T) {
	r := NewRing[int](3)
	if r.Len() != 0 {
		t.Errorf("Len = %d, want 0", r.Len())
	}
	r.Push(1)
	if r.Len() != 1 {
		t.Errorf("Len = %d, want 1", r.Len())
	}
	r.Push(2)
	r.Push(3)
	if r.Len() != 3 {
		t.Errorf("Len = %d, want 3", r.Len())
	}
	r.Push(4) // overflow
	if r.Len() != 3 {
		t.Errorf("Len = %d after overflow, want 3", r.Len())
	}
}

func TestRing_Cap(t *testing.T) {
	r := NewRing[int](42)
	if r.Cap() != 42 {
		t.Errorf("Cap = %d, want 42", r.Cap())
	}
}

func TestRing_ZeroCapacity(t *testing.T) {
	r := NewRing[int](0)
	if r.Cap() != 1 {
		t.Errorf("Cap = %d, want 1 (should normalize 0 to 1)", r.Cap())
	}
	r.Push(99)
	got := r.GetAll()
	if len(got) != 1 || got[0] != 99 {
		t.Errorf("expected [99], got %v", got)
	}
}

func TestRing_Empty(t *testing.T) {
	r := NewRing[int](5)
	got := r.GetAll()
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
	got = r.GetRange(0, 10)
	if len(got) != 0 {
		t.Errorf("expected empty slice from GetRange on empty buffer, got %v", got)
	}
}

func TestRing_ConcurrentAccess(t *testing.T) {
	r := NewRing[int](100)
	var wg sync.WaitGroup

	// Concurrent writers.
	for w := 0; w < 10; w++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				r.Push(base*100 + i)
			}
		}(w)
	}

	// Concurrent readers.
	for rd := 0; rd < 5; rd++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				_ = r.GetAll()
				_ = r.GetRange(0, 10)
				_ = r.Len()
			}
		}()
	}

	wg.Wait()

	// After all writes the buffer should be full.
	if r.Len() != 100 {
		t.Errorf("Len = %d after concurrent writes, want 100", r.Len())
	}
}
