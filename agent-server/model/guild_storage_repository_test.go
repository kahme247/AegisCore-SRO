package model

import (
	"errors"
	"testing"
)

func TestValidateGuildStorageAccessRequiresLevelAndPermission(t *testing.T) {
	for _, test := range []struct {
		name   string
		rights bool
		level  byte
		want   error
	}{
		{"allowed", true, 2, nil},
		{"guild too low", true, 1, ErrGuildStorageAccessDenied},
		{"member permission denied", false, 5, ErrGuildStorageAccessDenied},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := validateGuildStorageAccess(test.rights, test.level); !errors.Is(got, test.want) {
				t.Fatalf("error = %v, want %v", got, test.want)
			}
		})
	}
}

func TestValidateGuildStorageSlotsEnforcesChestAndInventoryBounds(t *testing.T) {
	if err := validateGuildStorageSlots(0, 149, 150); err != nil {
		t.Fatalf("valid chest move: %v", err)
	}
	for _, slots := range [][3]byte{{0, 0, 150}, {150, 0, 150}, {0, 150, 150}, {0, 1, 0}} {
		if err := validateGuildStorageSlots(slots[0], slots[1], slots[2]); !errors.Is(err, ErrGuildStorageConflict) {
			t.Fatalf("slots %v error = %v", slots, err)
		}
	}
	if err := validateGuildInventorySlot(13); err != nil {
		t.Fatalf("valid inventory slot: %v", err)
	}
	if err := validateGuildInventorySlot(12); !errors.Is(err, ErrGuildStorageConflict) {
		t.Fatalf("equipment slot error = %v", err)
	}
}
