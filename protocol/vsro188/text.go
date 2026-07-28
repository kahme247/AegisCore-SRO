package vsro188

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

var ErrInvalidText = errors.New("invalid client text")

type TextRules struct {
	Field          string
	MaxRunes       int
	AllowEmpty     bool
	RequireTrimmed bool
}

// ValidateText checks semantic constraints that do not belong in the generic
// length-prefixed packet string codec. It validates without normalizing input.
func ValidateText(value string, rules TextRules) error {
	field := rules.Field
	if field == "" {
		field = "text"
	}
	fail := func(reason string) error {
		return fmt.Errorf("%w: %s %s", ErrInvalidText, field, reason)
	}

	if !utf8.ValidString(value) {
		return fail("is not valid UTF-8")
	}
	if strings.IndexByte(value, 0) >= 0 {
		return fail("contains NUL")
	}
	if len(value) > math.MaxUint16 {
		return fail("exceeds the wire length limit")
	}
	if !rules.AllowEmpty && strings.TrimSpace(value) == "" {
		return fail("is empty")
	}
	if rules.RequireTrimmed && strings.TrimSpace(value) != value {
		return fail("has leading or trailing whitespace")
	}
	if rules.MaxRunes > 0 && utf8.RuneCountInString(value) > rules.MaxRunes {
		return fail(fmt.Sprintf("exceeds %d characters", rules.MaxRunes))
	}
	return nil
}
