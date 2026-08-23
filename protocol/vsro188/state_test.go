package vsro188

import (
	"errors"
	"testing"
)

func TestTypedIDsUseIndependentNamespaces(t *testing.T) {
	account := AccountID(11)
	character := CharacterDBID(22)
	jid := JID(22)
	entity := EntityUID(33)
	shard := ShardID(44)
	region := RegionID(0x8001)

	if got, want := uint32(account), uint32(11); got != want {
		t.Fatalf("account ID = %d, want %d", got, want)
	}
	if got, want := uint32(character), uint32(22); got != want {
		t.Fatalf("character DB ID = %d, want %d", got, want)
	}
	if got, want := uint32(jid), uint32(character); got != want {
		t.Fatalf("JID = %d, want %d", got, want)
	}
	if got, want := uint32(entity), uint32(33); got != want {
		t.Fatalf("entity UID = %d, want %d", got, want)
	}
	if got, want := uint16(shard), uint16(44); got != want {
		t.Fatalf("shard ID = %d, want %d", got, want)
	}
	if got, want := uint16(region), uint16(0x8001); got != want {
		t.Fatalf("region ID = 0x%04X, want 0x%04X", got, want)
	}
}

func TestStateValuesMatchTypedDomainContract(t *testing.T) {
	tests := []struct {
		name string
		got  uint8
		want uint8
	}{
		{"life spawning", uint8(LifeSpawning), 0},
		{"life alive", uint8(LifeAlive), 1},
		{"life dead", uint8(LifeDead), 2},
		{"life gone", uint8(LifeGone), 3},
		{"body normal", uint8(BodyNormal), 0},
		{"body hwan", uint8(BodyHwan), 1},
		{"body untouchable", uint8(BodyUntouchable), 2},
		{"body gm invincible", uint8(BodyGMInvincible), 3},
		{"body gm untouchable", uint8(BodyGMUntouchable), 4},
		{"body gm invisible", uint8(BodyGMInvisible), 5},
		{"body berserker", uint8(BodyBerserker), 1},
		{"body stealth", uint8(BodyStealth), 6},
		{"body invisible", uint8(BodyInvisible), 7},
		{"motion stand", uint8(MotionStand), 0},
		{"motion walk", uint8(MotionWalk), 1},
		{"motion run", uint8(MotionRun), 2},
		{"motion sit", uint8(MotionSit), 3},
		{"battle peace", uint8(BattlePeace), 0},
		{"battle", uint8(BattleBattle), 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.got != test.want {
				t.Fatalf("value = %d, want %d", test.got, test.want)
			}
		})
	}
}

func TestTranslateClientBodyRequest(t *testing.T) {
	tests := []struct {
		name    string
		request ClientBodyRequest
		want    BodyMode
		wantErr error
	}{
		{
			name:    "normal",
			request: ClientBodyRequestNormal,
			want:    BodyNormal,
		},
		{
			name:    "berserk maps to authoritative berserker",
			request: ClientBodyRequestBerserk,
			want:    BodyBerserker,
		},
		{
			name:    "rejects privileged client value",
			request: ClientBodyRequest(2),
			wantErr: ErrClientBodyRequestRejected,
		},
		{
			name:    "rejects unknown client value",
			request: ClientBodyRequest(255),
			wantErr: ErrClientBodyRequestRejected,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := TranslateClientBodyRequest(test.request)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("body mode = %d, want %d", got, test.want)
			}
		})
	}
}
