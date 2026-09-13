package model

import (
	"context"
	"errors"
	"math"
	"testing"
)

func TestSafeTradeStateRejectsUnavailableRepository(t *testing.T) {
	var repository *SQLSafeTradeRepository
	if _, err := repository.State(context.Background(), 1); !errors.Is(err, ErrSafeTradeInvalidRequest) {
		t.Fatalf("nil repository state error = %v, want ErrSafeTradeInvalidRequest", err)
	}
}

func TestSpecialtyQuotationRewardUsesStockMargin(t *testing.T) {
	reward, err := SpecialtyQuotationReward(191, 383, 40)
	if err != nil {
		t.Fatalf("SpecialtyQuotationReward returned error: %v", err)
	}
	if reward != 7680 {
		t.Fatalf("reward = %d, want 7680", reward)
	}
}

func TestSafeTradeNormalizedScoreMatchesStockRationalAndTieRounding(t *testing.T) {
	for _, test := range []struct {
		name                       string
		quantity, factor, capacity uint64
		rarity                     int
		want                       uint64
	}{
		{name: "empty cargo", factor: 470, rarity: 1, want: 0},
		{name: "positive fraction floors to one", quantity: 1, factor: 470, rarity: 1, want: 1},
		{name: "rarity one forces forty capacity", quantity: 689, factor: 470, rarity: 1, capacity: 1, want: 408},
		{name: "rarity two forces thirty capacity", quantity: 689, factor: 470, rarity: 2, capacity: 99, want: 306},
		{name: "other rarity uses reference capacity", quantity: 689, factor: 470, rarity: 0, capacity: 20, want: 204},
		// 498 * 1.25 is 622.5 and the x87 helper rounds the even tie to 622.
		{name: "level denominator uses nearest even", quantity: 622, factor: 498, rarity: 1, want: 348},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := SafeTradeNormalizedScore(test.quantity, test.factor, test.rarity, test.capacity)
			if err != nil {
				t.Fatalf("SafeTradeNormalizedScore returned error: %v", err)
			}
			if got != test.want {
				t.Fatalf("score = %d, want %d", got, test.want)
			}
		})
	}
}

func TestSafeTradeScaleResetMatchesStockSetter(t *testing.T) {
	tests := []struct {
		previous byte
		current  byte
		want     bool
	}{
		{previous: 1, current: 2, want: true},
		{previous: 0, current: 2, want: true},
		{previous: 2, current: 2},
		{previous: 2, current: 1},
	}
	for _, test := range tests {
		if got := SafeTradeScaleRequiresReset(test.previous, test.current); got != test.want {
			t.Fatalf("SafeTradeScaleRequiresReset(%d, %d) = %t, want %t", test.previous, test.current, got, test.want)
		}
	}
}

func TestSafeTradeAdmissionMatchesStockOptionAndLimitMatrix(t *testing.T) {
	tests := []struct {
		name          string
		state         SafeTradeState
		option, limit byte
		scale         byte
		wantCode      byte
		wantCount     byte
		wantStatus    byte
	}{
		{name: "counted admission increments", state: SafeTradeState{AbleCount: 2}, option: 0, limit: 8, scale: 1, wantCode: 0x50, wantCount: 3, wantStatus: 1},
		{name: "already active does not increment", state: SafeTradeState{AbleCount: 2, Status: 1}, option: 0, limit: 8, scale: 1, wantCode: 0x50, wantCount: 2, wantStatus: 1},
		{name: "counted limit exhausted", state: SafeTradeState{AbleCount: 8, Status: 1}, option: 0, limit: 8, scale: 1, wantCode: 0x41, wantCount: 8},
		{name: "disabled resets", state: SafeTradeState{AbleCount: 2, Status: 1}, option: 1, limit: 8, scale: 1, wantCode: 0x40, wantCount: 2},
		{name: "unlimited does not increment", state: SafeTradeState{AbleCount: 2}, option: 2, limit: 8, scale: 1, wantCode: 0x50, wantCount: 2, wantStatus: 1},
		{name: "wrong scale resets", state: SafeTradeState{AbleCount: 2, Status: 1}, option: 0, limit: 8, scale: 2, wantCode: 0x40, wantCount: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := applySafeTradeAdmission(test.state, test.option, test.limit, test.scale)
			if err != nil {
				t.Fatal(err)
			}
			if got.Code != test.wantCode || got.State.AbleCount != test.wantCount || got.State.Status != test.wantStatus {
				t.Fatalf("admission = %#v", got)
			}
		})
	}
}

func TestSafeTradeNormalizedScoreRejectsMissingReferenceInputs(t *testing.T) {
	for _, test := range []struct {
		factor, capacity uint64
		rarity           int
	}{
		{rarity: 1},
		{factor: 470},
	} {
		if _, err := SafeTradeNormalizedScore(1, test.factor, test.rarity, test.capacity); !errors.Is(err, ErrSafeTradeInvalidRequest) {
			t.Fatalf("error = %v, want ErrSafeTradeInvalidRequest", err)
		}
	}
}

func TestSpecialtyTriJobRewardRoundsStockTenPercent(t *testing.T) {
	for _, test := range []struct {
		payout uint64
		want   uint64
	}{{0, 0}, {4, 0}, {5, 1}, {14, 1}, {15, 2}, {25, 3}, {math.MaxUint64, math.MaxUint64/10 + 1}} {
		if got := SpecialtyTriJobReward(test.payout); got != test.want {
			t.Fatalf("SpecialtyTriJobReward(%d) = %d, want %d", test.payout, got, test.want)
		}
	}
}

func TestSpecialtyDeliveryProgressionMatchesTraderScaleFactors(t *testing.T) {
	if got := SpecialtyDeliveryJobExperience(101, 1); got != 80 {
		t.Fatalf("scale-one job EXP = %d, want 80", got)
	}
	if got := SpecialtyDeliveryJobExperience(101, 2); got != 50 {
		t.Fatalf("higher-scale job EXP = %d, want 50", got)
	}
	if got := SpecialtyDeliveryJobExperience(0, 1); got != 1 {
		t.Fatalf("minimum job EXP = %d, want 1", got)
	}
	if got := SpecialtyDeliveryContribution(109); got != 10 {
		t.Fatalf("weekly contribution = %d, want 10", got)
	}
}

func TestSpecialtyQuotationRewardRejectsInvalidInputs(t *testing.T) {
	for _, test := range []struct {
		name          string
		base, current uint32
		quantity      uint16
	}{
		{name: "zero quantity", base: 191, current: 383},
		{name: "quotation below base", base: 383, current: 191, quantity: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := SpecialtyQuotationReward(test.base, test.current, test.quantity)
			if !errors.Is(err, ErrSafeTradeInvalidRequest) {
				t.Fatalf("error = %v, want ErrSafeTradeInvalidRequest", err)
			}
		})
	}
}

func TestSpecialtyCurrentQuotationUsesStockDeltaSteps(t *testing.T) {
	quotation, err := SpecialtyCurrentQuotation(191, 50000, 250, 42499)
	if err != nil {
		t.Fatalf("SpecialtyCurrentQuotation returned error: %v", err)
	}
	// C++ integer division truncates -7501/250 toward zero to -30; the
	// pinned binary then applies its exact 2.0 quotation step.
	if quotation != 251 {
		t.Fatalf("quotation = %d, want 251", quotation)
	}
}

func TestSpecialtyCurrentQuotationRejectsInvalidReferenceRows(t *testing.T) {
	for _, test := range []struct {
		name                               string
		basePrice                          uint32
		baseStock, fluctuate, currentStock int
	}{
		{name: "zero base price", baseStock: 50000, fluctuate: 250, currentStock: 42499},
		{name: "zero fluctuation", basePrice: 191, baseStock: 50000, currentStock: 42499},
		{name: "negative current stock", basePrice: 191, baseStock: 50000, fluctuate: 250, currentStock: -1},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := SpecialtyCurrentQuotation(test.basePrice, test.baseStock, test.fluctuate, test.currentStock)
			if !errors.Is(err, ErrSafeTradeInvalidRequest) {
				t.Fatalf("error = %v, want ErrSafeTradeInvalidRequest", err)
			}
		})
	}
}
