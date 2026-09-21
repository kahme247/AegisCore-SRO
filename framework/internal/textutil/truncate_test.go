package textutil

import "testing"

func TestTruncateUTF8(t *testing.T) {
	tests := []struct {
		name  string
		value string
		limit int
		want  string
	}{
		{name: "ASCII", value: "abcdef", limit: 3, want: "abc"},
		{name: "whole multibyte rune", value: "A€B", limit: 4, want: "A€"},
		{name: "partial multibyte rune", value: "A€B", limit: 3, want: "A"},
		{name: "zero limit", value: "A", limit: 0, want: ""},
		{name: "invalid input within limit", value: string([]byte{'A', 0xff}), limit: 2, want: string([]byte{'A', 0xff})},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := TruncateUTF8(test.value, test.limit); got != test.want {
				t.Errorf("TruncateUTF8(%q, %d) = %q, want %q", test.value, test.limit, got, test.want)
			}
		})
	}
}

func TestTruncateUTF8NegativeLimitPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("TruncateUTF8() did not panic for a negative limit")
		}
	}()

	TruncateUTF8("value", -1)
}
