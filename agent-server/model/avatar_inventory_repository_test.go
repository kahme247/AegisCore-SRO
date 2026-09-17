package model

import (
	"testing"
	"time"
)

func TestDevilSpiritDurationUsesStockSecondsUnit(t *testing.T) {
	now := time.Unix(1_750_000_000, 0)
	const stockNasrunDuration = 2_419_200

	if got, want := devilSpiritExpiration(now, stockNasrunDuration), now.Add(28*24*time.Hour); !got.Equal(want) {
		t.Fatalf("Devil Spirit expiration = %v, want %v", got, want)
	}
}

func TestDevilSpiritDurationRepairsFreshZeroDataFromReference(t *testing.T) {
	const stockNasrunDuration = 2_419_200

	if got, initialize := devilSpiritDuration(0, 0, 0, stockNasrunDuration); got != stockNasrunDuration || !initialize {
		t.Fatalf("fresh Devil Spirit duration = %d/%v, want %d/true", got, initialize, stockNasrunDuration)
	}
	if got, initialize := devilSpiritDuration(0, 1, 0, stockNasrunDuration); got != 0 || initialize {
		t.Fatalf("expired Devil Spirit duration = %d/%v, want 0/false", got, initialize)
	}
}
