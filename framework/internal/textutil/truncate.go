package textutil

import "unicode/utf8"

// TruncateUTF8 limits value to limit bytes without returning a partial UTF-8 rune.
func TruncateUTF8(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
