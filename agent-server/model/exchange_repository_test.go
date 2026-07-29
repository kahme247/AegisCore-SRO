package model

import (
	"errors"
	"testing"
)

func TestValidateExchangeOfferRejectsStaleAndNonTradableItems(t *testing.T) {
	state := &lockedExchangeCharacter{inventory: map[byte]lockedExchangeItem{
		13: {slot: 13, itemID: 100, tradable: true},
		14: {slot: 14, itemID: 200, tradable: false},
	}}

	tests := []struct {
		name  string
		offer ExchangeOffer
	}{
		{name: "stale item identity", offer: ExchangeOffer{Items: []ExchangeOfferedItem{{SourceSlot: 13, ExchangeSlot: 0, ItemID: 999}}}},
		{name: "non tradable", offer: ExchangeOffer{Items: []ExchangeOfferedItem{{SourceSlot: 14, ExchangeSlot: 0, ItemID: 200}}}},
		{name: "equipment slot", offer: ExchangeOffer{Items: []ExchangeOfferedItem{{SourceSlot: 12, ExchangeSlot: 0, ItemID: 100}}}},
		{name: "duplicate exchange slot", offer: ExchangeOffer{Items: []ExchangeOfferedItem{{SourceSlot: 13, ExchangeSlot: 0, ItemID: 100}, {SourceSlot: 13, ExchangeSlot: 0, ItemID: 100}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateExchangeOffer(test.offer, state); !errors.Is(err, ErrExchangeConflict) {
				t.Fatalf("validateExchangeOffer() error = %v, want ErrExchangeConflict", err)
			}
		})
	}
}

func TestExchangeDestinationSlotsReusesOutgoingSlots(t *testing.T) {
	inventory := make(map[byte]lockedExchangeItem)
	for slot := byte(13); slot < BaseInventorySize; slot++ {
		inventory[slot] = lockedExchangeItem{slot: slot, itemID: int(slot)}
	}
	outgoing := []ExchangeOfferedItem{{SourceSlot: 20, ItemID: 20}, {SourceSlot: 21, ItemID: 21}}

	slots, err := exchangeDestinationSlots(inventory, outgoing, 2)
	if err != nil {
		t.Fatalf("exchangeDestinationSlots() error = %v", err)
	}
	if len(slots) != 2 || slots[0] != 20 || slots[1] != 21 {
		t.Fatalf("exchangeDestinationSlots() = %v, want [20 21]", slots)
	}
	if _, err = exchangeDestinationSlots(inventory, outgoing[:1], 2); !errors.Is(err, ErrExchangeInventoryFull) {
		t.Fatalf("exchangeDestinationSlots() capacity error = %v, want ErrExchangeInventoryFull", err)
	}
}
