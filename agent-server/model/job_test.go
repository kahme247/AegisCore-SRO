package model

import (
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestPlayerJobStateNormalizesInactiveState(t *testing.T) {
	player := &Player{}
	if job := player.GetJob(); job.Type != PlayerJobNone || job.Level != 0 {
		t.Fatalf("zero player job = %+v", job)
	}
	player.SetJob(PlayerJob{Type: PlayerJobHunter, Level: 5, Experience: 123, Alias: "Aegis"})
	if job := player.GetJob(); job.Type != PlayerJobHunter || job.Level != 5 || job.Alias != "Aegis" {
		t.Fatalf("active player job = %+v", job)
	}
	player.SetJob(PlayerJob{Type: 9, Level: 8})
	if job := player.GetJob(); job.Type != PlayerJobNone || job.Level != 0 {
		t.Fatalf("invalid player job = %+v", job)
	}
}

func TestWritePlayerJobStateUsesConditionalLevel(t *testing.T) {
	inactive := network.EmptyPacket()
	writePlayerJobState(&inactive, PlayerJob{})
	if got := inactive.Payload(); len(got) != 1 || got[0] != PlayerJobNone {
		t.Fatalf("inactive payload = %x", got)
	}
	active := network.EmptyPacket()
	writePlayerJobState(&active, PlayerJob{Type: PlayerJobThief, Level: 7})
	if got := active.Payload(); len(got) != 2 || got[0] != PlayerJobThief || got[1] != 7 {
		t.Fatalf("active payload = %x", got)
	}
}

func TestJobFeePoliciesMatchRecoveredServerArithmetic(t *testing.T) {
	if _, ok := jobEnrollmentFee(19); ok {
		t.Fatal("level 19 accepted for job enrollment")
	}
	if fee, ok := jobEnrollmentFee(20); !ok || fee != 0 {
		t.Fatalf("level 20 enrollment fee = %d, %t", fee, ok)
	}
	if fee, ok := jobEnrollmentFee(80); !ok || fee != 300000 {
		t.Fatalf("level 80 enrollment fee = %d, %t", fee, ok)
	}
	if fee, ok := jobLeaveFee(80); !ok || fee != 4000000 {
		t.Fatalf("level 80 leave fee = %d, %t", fee, ok)
	}
}

func TestOnlyTraderOwnsSafeTradeState(t *testing.T) {
	for _, test := range []struct {
		jobType byte
		want    bool
	}{
		{jobType: JobTrader, want: true},
		{jobType: JobThief},
		{jobType: JobHunter},
		{jobType: JobNone},
	} {
		if got := jobUsesSafeTradeState(test.jobType); got != test.want {
			t.Fatalf("jobUsesSafeTradeState(%d) = %t, want %t", test.jobType, got, test.want)
		}
	}
}
