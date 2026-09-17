package model

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func TestGuildWarProposalValidationRunsBeforeDatabaseAccess(t *testing.T) {
	repository := (*SQLSocialRepository)(nil)
	for _, test := range []struct {
		name     string
		actor    int
		proposal GuildWarProposal
	}{
		{"missing actor", 0, GuildWarProposal{TargetGuildName: "Target", WarPeriodID: 10 << 20}},
		{"missing target", 1, GuildWarProposal{WarPeriodID: 10 << 20}},
		{"wager too large", 1, GuildWarProposal{TargetGuildName: "Target", WarPeriodID: 10 << 20, Wager: GuildWarMaximumWager + 1}},
		{"invalid period", 1, GuildWarProposal{TargetGuildName: "Target", WarPeriodID: 1}},
		{"invalid victory target", 1, GuildWarProposal{TargetGuildName: "Target", WarPeriodID: 10 << 20, VictoryPointID: 8}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := repository.ProposeGuildWar(context.Background(), test.actor, test.proposal); !errors.Is(err, ErrSocialInvalidRequest) {
				t.Fatalf("error = %v, want ErrSocialInvalidRequest", err)
			}
		})
	}
}

func TestGuildWarVictoryTargetsMatchGameServerTable(t *testing.T) {
	want := []uint32{0, 5_000, 10_000, 50_000, 100_000, 150_000, 250_000, 300_000}
	for id, target := range want {
		got, err := guildWarVictoryTarget(byte(id))
		if err != nil || got != target {
			t.Fatalf("target %d = %d, error = %v", id, got, err)
		}
	}
}

func TestGuildWarWinnerGoldPaysCombinedEscrow(t *testing.T) {
	gold, err := guildWarWinnerGold(100, 50)
	if err != nil || gold != 200 {
		t.Fatalf("winner gold = %d, error = %v, want 200", gold, err)
	}
	if _, err = guildWarWinnerGold(math.MaxUint64, 1); !errors.Is(err, ErrSocialConflict) {
		t.Fatalf("overflow error = %v, want ErrSocialConflict", err)
	}
}

func TestGuildWarRepositoryUnavailableAfterValidInput(t *testing.T) {
	repository := (*SQLSocialRepository)(nil)
	if _, err := repository.ProposeGuildWar(context.Background(), 1, GuildWarProposal{TargetGuildName: "Target", WarPeriodID: 10 << 20}); !errors.Is(err, ErrSocialRepositoryUnavailable) {
		t.Fatalf("proposal error = %v, want ErrSocialRepositoryUnavailable", err)
	}
	if _, err := repository.AnswerGuildWar(context.Background(), 1, true); !errors.Is(err, ErrSocialRepositoryUnavailable) {
		t.Fatalf("answer error = %v, want ErrSocialRepositoryUnavailable", err)
	}
	if _, err := repository.EndGuildWar(context.Background(), 1, 7); !errors.Is(err, ErrSocialRepositoryUnavailable) {
		t.Fatalf("end error = %v, want ErrSocialRepositoryUnavailable", err)
	}
	if _, err := repository.ClaimGuildWarReward(context.Background(), 1, 7); !errors.Is(err, ErrSocialRepositoryUnavailable) {
		t.Fatalf("claim error = %v, want ErrSocialRepositoryUnavailable", err)
	}
}

func TestGuildWarPeriodDurationMatchesClientPacking(t *testing.T) {
	duration, unlimited, err := guildWarPeriodDuration(2<<10 | 3<<15 | 40<<20)
	if err != nil || unlimited || duration != 51*time.Hour+40*time.Minute {
		t.Fatalf("duration = %v, unlimited = %v, error = %v", duration, unlimited, err)
	}
}
