package model

import "testing"

func TestSpecialtyPickupAllowedMatchesStockJobMatrix(t *testing.T) {
	tests := []struct {
		name    string
		jobType byte
		alias   string
		creator string
		want    bool
	}{
		{name: "trader own", jobType: JobTrader, alias: "Merchant", creator: "merchant", want: true},
		{name: "trader foreign", jobType: JobTrader, alias: "Merchant", creator: "Other"},
		{name: "thief foreign", jobType: JobThief, alias: "Robber", creator: "Merchant", want: true},
		{name: "thief own", jobType: JobThief, alias: "Robber", creator: "robber"},
		{name: "hunter", jobType: JobHunter, alias: "Hunter", creator: "Merchant"},
		{name: "inactive", jobType: JobNone, alias: "", creator: "Merchant"},
		{name: "missing creator", jobType: JobThief, alias: "Robber"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := SpecialtyPickupAllowed(test.jobType, test.alias, test.creator); got != test.want {
				t.Fatalf("SpecialtyPickupAllowed(%d, %q, %q) = %t, want %t", test.jobType, test.alias, test.creator, got, test.want)
			}
		})
	}
}
