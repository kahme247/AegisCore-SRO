package model

import "testing"

func TestAvatarBlessingMaximumMatchesBinaryRules(t *testing.T) {
	tests := []struct {
		name      string
		paramType uint32
		packed    uint32
		want      uint32
	}{
		{"ordinary decoded maximum", 0x1234, 3<<16 | 9, 9},
		{"invalid range defaults one", 0x1234, 9<<16 | 3, 1},
		{"zero defaults one", 0x1234, 0, 1},
		{"athanasia cap", 0x61746861, 0, 5},
		{"luck cap", 0x6c75636b, 0, 5},
		{"solid cap", 0x736f6c69, 0, 5},
		{"astral cap", 0x61737472, 0, 5},
		{"repair cap", 0x00726570, 0, 6},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := avatarBlessingMaximum(test.paramType, test.packed); got != test.want {
				t.Fatalf("avatarBlessingMaximum(%#x, %#x) = %d, want %d", test.paramType, test.packed, got, test.want)
			}
		})
	}
}

func TestAvatarMagicOptionAssignmentsMatchTypeID4(t *testing.T) {
	avatar := func(typeID4 int) RefItem {
		return RefItem{RefObject: RefObject{TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 13, TypeID4: typeID4}}}
	}
	if !avatarOptionApplies(avatar(1), "MATTR_AVATAR_DARA") || avatarOptionApplies(avatar(1), "MATTR_AVATAR_HP") {
		t.Fatal("hat assignment did not enforce the restored option set")
	}
	if !avatarOptionApplies(avatar(2), "MATTR_AVATAR_HP") || avatarOptionApplies(avatar(2), "MATTR_STR_AVATAR") {
		t.Fatal("dress assignment did not enforce the restored option set")
	}
	if !avatarOptionApplies(avatar(3), "MATTR_AVATAR_HPRG") || avatarOptionApplies(avatar(3), "MATTR_AVATAR_STR") {
		t.Fatal("attach assignment did not enforce the restored option set")
	}
	if !avatarOptionApplies(avatar(4), "MATTR_STR_AVATAR") || avatarOptionApplies(avatar(4), "MATTR_AVATAR_STR") {
		t.Fatal("devil-spirit assignment did not enforce the restored option set")
	}
	first, ok := randomAvatarOption(avatar(3), func() float64 { return 0 })
	last, lastOK := randomAvatarOption(avatar(3), func() float64 { return 0.999999 })
	if !ok || !lastOK || first != "MATTR_AVATAR_HR" || last != "MATTR_AVATAR_HPRG" {
		t.Fatalf("attach random options = %q/%q (%v/%v)", first, last, ok, lastOK)
	}
}
