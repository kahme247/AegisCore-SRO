package model

import "testing"

func TestCOSSaveAcceptsMatchedUnchangedRow(t *testing.T) {
	for _, test := range []struct {
		affected int64
		valid    bool
	}{
		{affected: 0, valid: true},
		{affected: 1, valid: true},
		{affected: 2, valid: false},
		{affected: -1, valid: false},
	} {
		if got := validCOSSaveAffected(test.affected); got != test.valid {
			t.Fatalf("validCOSSaveAffected(%d) = %t, want %t", test.affected, got, test.valid)
		}
	}
}
