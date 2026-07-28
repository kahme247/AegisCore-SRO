package vsro188

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateText(t *testing.T) {
	tests := []struct {
		name  string
		value string
		rules TextRules
		valid bool
	}{
		{name: "ascii", value: "Market", rules: TextRules{RequireTrimmed: true}, valid: true},
		{name: "unicode", value: "Silk road", rules: TextRules{}, valid: true},
		{name: "invalid UTF-8", value: string([]byte{0xff}), rules: TextRules{}, valid: false},
		{name: "NUL", value: "bad\x00name", rules: TextRules{}, valid: false},
		{name: "blank", value: " \t", rules: TextRules{}, valid: false},
		{name: "padded name", value: " Market ", rules: TextRules{RequireTrimmed: true}, valid: false},
		{name: "message preserves padding", value: " hello ", rules: TextRules{}, valid: true},
		{name: "rune limit", value: "abcd", rules: TextRules{MaxRunes: 3}, valid: false},
		{name: "wire limit", value: strings.Repeat("a", 1<<16), rules: TextRules{}, valid: false},
		{name: "allowed empty", value: "", rules: TextRules{AllowEmpty: true}, valid: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateText(test.value, test.rules)
			if test.valid && err != nil {
				t.Fatalf("ValidateText() error = %v", err)
			}
			if !test.valid && !errors.Is(err, ErrInvalidText) {
				t.Fatalf("ValidateText() error = %v, want ErrInvalidText", err)
			}
		})
	}
}
