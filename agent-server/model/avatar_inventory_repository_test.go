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
