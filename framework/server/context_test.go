package server

import "testing"

func TestNewContextUsesUsableNonZeroHandshakeValues(t *testing.T) {
	context := NewContext()
	if context.Prime < 3 {
		t.Fatalf("Prime = %d, want at least 3", context.Prime)
	}
	if context.Generator < 2 {
		t.Fatalf("Generator = %d, want at least 2", context.Generator)
	}
	if context.Private == 0 {
		t.Fatal("Private must not be zero")
	}
	if context.InitialBlowfishKey == 0 {
		t.Fatal("InitialBlowfishKey must not be zero")
	}
	if context.HandshakeBlowfishKey == 0 {
		t.Fatal("HandshakeBlowfishKey must not be zero")
	}
}
