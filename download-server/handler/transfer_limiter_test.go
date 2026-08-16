package handler

import (
	"errors"
	"testing"
)

func TestTransferLimiterEnforcesPerClientAndGlobalLimits(t *testing.T) {
	limiter, err := NewTransferLimiter(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	releaseA, err := limiter.Acquire("a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := limiter.Acquire("a"); !errors.Is(err, ErrTransferLimit) {
		t.Fatalf("second client-a acquire error = %v", err)
	}
	releaseB, err := limiter.Acquire("b")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := limiter.Acquire("c"); !errors.Is(err, ErrTransferLimit) {
		t.Fatalf("global-limit acquire error = %v", err)
	}
	releaseA()
	releaseA()
	releaseC, err := limiter.Acquire("c")
	if err != nil {
		t.Fatalf("Acquire() after release = %v", err)
	}
	releaseB()
	releaseC()
}

func TestTransferLimiterRejectsInvalidConfiguration(t *testing.T) {
	for _, limits := range [][2]int{{0, 1}, {1, 0}, {1, 2}} {
		if _, err := NewTransferLimiter(limits[0], limits[1]); err == nil {
			t.Fatalf("NewTransferLimiter(%d, %d) accepted invalid limits", limits[0], limits[1])
		}
	}
}
