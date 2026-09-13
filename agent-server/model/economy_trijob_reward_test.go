package model

import "testing"

func TestTriJobRewardTypeMatchesStockAdapter(t *testing.T) {
	tests := []struct {
		name    string
		jobType byte
		want    byte
		ok      bool
	}{
		{name: "trader", jobType: JobTrader, want: 3, ok: true},
		{name: "thief", jobType: JobThief, want: 2, ok: true},
		{name: "hunter", jobType: JobHunter},
		{name: "none", jobType: JobNone},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := triJobRewardType(test.jobType)
			if got != test.want || ok != test.ok {
				t.Fatalf("triJobRewardType(%d) = (%d, %t), want (%d, %t)", test.jobType, got, ok, test.want, test.ok)
			}
		})
	}
}
