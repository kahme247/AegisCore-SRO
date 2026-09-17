package service

import (
	"errors"
	"sort"
	"sync"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

type dimensionHoleAdmissionInstancesFake struct {
	mu    sync.RWMutex
	byUID map[uint32]DimensionHolePortalInstance
}

func (f *dimensionHoleAdmissionInstancesFake) Instance(uid uint32) (DimensionHolePortalInstance, bool) {
	f.mu.RLock()
	instance, ok := f.byUID[uid]
	f.mu.RUnlock()
	return instance, ok
}

type dimensionHoleAdmissionPlayersFake struct {
	mu    sync.RWMutex
	byUID map[uint32]*model.Player
}

func (f *dimensionHoleAdmissionPlayersFake) GetPlayerByUniqueId(uid uint32) (*model.Player, error) {
	f.mu.RLock()
	player := f.byUID[uid]
	f.mu.RUnlock()
	if player == nil {
		return nil, errors.New("player missing")
	}
	return player, nil
}

func TestDimensionHoleAdmissionServiceAdmitsPartyMembersIdempotentlyAndLeaves(t *testing.T) {
	definition := model.DimensionHoleCatalog()[0]
	owner := dimensionHoleAdmissionTestPlayer(1, 100, int(definition.MinLevel))
	member := dimensionHoleAdmissionTestPlayer(2, 200, int(definition.MaxLevel))
	party := dimensionHoleAdmissionTestParty(owner.GetUniqueID(), owner, member)
	owner.Party = party
	member.Party = party
	instance := dimensionHoleAdmissionTestInstance(t, definition, owner, 501, 1)
	instances := &dimensionHoleAdmissionInstancesFake{byUID: map[uint32]DimensionHolePortalInstance{501: instance}}
	players := &dimensionHoleAdmissionPlayersFake{byUID: map[uint32]*model.Player{100: owner, 200: member}}
	admissions, err := NewDimensionHoleAdmissionService(instances, players)
	if err != nil {
		t.Fatal(err)
	}

	first, err := admissions.Admit(501, member)
	if err != nil {
		t.Fatalf("Admit(member) error = %v", err)
	}
	second, err := admissions.Admit(501, member)
	if err != nil || second != first {
		t.Fatalf("repeated Admit(member) = %#v, %v; want %#v", second, err, first)
	}
	ownerAdmission, err := admissions.Admit(501, owner)
	if err != nil {
		t.Fatalf("Admit(owner) error = %v", err)
	}
	if first.PortalUID != 501 || first.PlayerUID != member.GetUniqueID() || first.OwnerUID != owner.GetUniqueID() || first.Scope != instance.Lease.Scope || first.Definition != definition {
		t.Fatalf("member admission = %#v", first)
	}
	if ownerAdmission.PlayerUID != owner.GetUniqueID() {
		t.Fatalf("owner admission = %#v", ownerAdmission)
	}
	if got := admissions.Members(501); len(got) != 2 || got[0] != 100 || got[1] != 200 {
		t.Fatalf("Members(501) = %v", got)
	}
	if got, ok := admissions.AdmissionForPlayer(200); !ok || got != first {
		t.Fatalf("AdmissionForPlayer(200) = %#v, %t", got, ok)
	}
	if err = admissions.Leave(200); err != nil {
		t.Fatalf("Leave(200) error = %v", err)
	}
	if _, ok := admissions.AdmissionForPlayer(200); ok {
		t.Fatal("left member retained admission")
	}
	if err = admissions.Leave(200); !errors.Is(err, ErrDimensionHoleAdmissionMissing) {
		t.Fatalf("second Leave(200) error = %v", err)
	}
}

func TestDimensionHoleAdmissionServiceRejectsInvalidIdentityPartyLevelAndSourceWorld(t *testing.T) {
	tests := []struct {
		name string
		want error
		edit func(*dimensionHoleAdmissionInstancesFake, *dimensionHoleAdmissionPlayersFake, *model.Player, *model.Player, *model.Party)
	}{
		{name: "owner missing", want: ErrDimensionHoleAdmissionOwner, edit: func(_ *dimensionHoleAdmissionInstancesFake, players *dimensionHoleAdmissionPlayersFake, _ *model.Player, _ *model.Player, _ *model.Party) {
			delete(players.byUID, 100)
		}},
		{name: "owner no longer master", want: ErrDimensionHoleAdmissionParty, edit: func(_ *dimensionHoleAdmissionInstancesFake, _ *dimensionHoleAdmissionPlayersFake, _ *model.Player, _ *model.Player, party *model.Party) {
			party.MasterUniqueID = 200
		}},
		{name: "member absent from party roster", want: ErrDimensionHoleAdmissionParty, edit: func(_ *dimensionHoleAdmissionInstancesFake, _ *dimensionHoleAdmissionPlayersFake, _ *model.Player, _ *model.Player, party *model.Party) {
			party.Members = []uint32{100}
		}},
		{name: "different party", want: ErrDimensionHoleAdmissionParty, edit: func(_ *dimensionHoleAdmissionInstancesFake, _ *dimensionHoleAdmissionPlayersFake, _ *model.Player, member *model.Player, _ *model.Party) {
			member.Party = &model.Party{MasterUniqueID: 200, Members: []uint32{200}}
		}},
		{name: "level below range", want: ErrDimensionHoleAdmissionLevel, edit: func(_ *dimensionHoleAdmissionInstancesFake, _ *dimensionHoleAdmissionPlayersFake, _ *model.Player, member *model.Player, _ *model.Party) {
			member.Level = 1
		}},
		{name: "free PVP voucher equipped", want: ErrDimensionHoleAdmissionState, edit: func(_ *dimensionHoleAdmissionInstancesFake, _ *dimensionHoleAdmissionPlayersFake, _ *model.Player, member *model.Player, _ *model.Party) {
			member.SetPVPCape(model.PvpCapeBlue)
		}},
		{name: "ride transport mounted", want: ErrDimensionHoleAdmissionState, edit: func(_ *dimensionHoleAdmissionInstancesFake, _ *dimensionHoleAdmissionPlayersFake, _ *model.Player, member *model.Player, _ *model.Party) {
			_ = member.COSState.ActivateTransport(900, 901, true)
		}},
		{name: "different source world", want: ErrDimensionHoleAdmissionInvalid, edit: func(_ *dimensionHoleAdmissionInstancesFake, _ *dimensionHoleAdmissionPlayersFake, _ *model.Player, member *model.Player, _ *model.Party) {
			member.SetGameWorldID(99)
		}},
		{name: "portal lifecycle missing", want: ErrDimensionHoleAdmissionInvalid, edit: func(instances *dimensionHoleAdmissionInstancesFake, _ *dimensionHoleAdmissionPlayersFake, _ *model.Player, _ *model.Player, _ *model.Party) {
			delete(instances.byUID, 501)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition := model.DimensionHoleCatalog()[0]
			owner := dimensionHoleAdmissionTestPlayer(1, 100, int(definition.MinLevel))
			member := dimensionHoleAdmissionTestPlayer(2, 200, int(definition.MinLevel))
			party := dimensionHoleAdmissionTestParty(owner.GetUniqueID(), owner, member)
			owner.Party = party
			member.Party = party
			instance := dimensionHoleAdmissionTestInstance(t, definition, owner, 501, 1)
			instances := &dimensionHoleAdmissionInstancesFake{byUID: map[uint32]DimensionHolePortalInstance{501: instance}}
			players := &dimensionHoleAdmissionPlayersFake{byUID: map[uint32]*model.Player{100: owner, 200: member}}
			test.edit(instances, players, owner, member, party)
			admissions, err := NewDimensionHoleAdmissionService(instances, players)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = admissions.Admit(501, member); !errors.Is(err, test.want) {
				t.Fatalf("Admit() error = %v; want %v", err, test.want)
			}
		})
	}
}

func TestDimensionHoleAdmissionServiceEnforcesCapacityAndCrossPortalOwnership(t *testing.T) {
	definitions := model.DimensionHoleCatalog()
	owner := dimensionHoleAdmissionTestPlayer(1, 100, int(definitions[0].MinLevel))
	members := make([]*model.Player, 0, 5)
	for index := 0; index < 5; index++ {
		members = append(members, dimensionHoleAdmissionTestPlayer(index+2, uint32(200+index), int(definitions[0].MinLevel)))
	}
	partyMembers := append([]*model.Player{owner}, members...)
	party := dimensionHoleAdmissionTestParty(owner.GetUniqueID(), partyMembers...)
	for _, player := range partyMembers {
		player.Party = party
	}
	first := dimensionHoleAdmissionTestInstance(t, definitions[0], owner, 501, 1)
	second := dimensionHoleAdmissionTestInstance(t, definitions[1], owner, 502, 1)
	instances := &dimensionHoleAdmissionInstancesFake{byUID: map[uint32]DimensionHolePortalInstance{501: first, 502: second}}
	playersByUID := map[uint32]*model.Player{owner.GetUniqueID(): owner}
	for _, player := range members {
		playersByUID[player.GetUniqueID()] = player
	}
	admissions, _ := NewDimensionHoleAdmissionService(instances, &dimensionHoleAdmissionPlayersFake{byUID: playersByUID})
	for index := 0; index < int(definitions[0].Capacity); index++ {
		if _, err := admissions.Admit(501, members[index]); err != nil {
			t.Fatalf("Admit(capacity member %d) error = %v", index, err)
		}
	}
	if _, err := admissions.Admit(501, members[4]); !errors.Is(err, ErrDimensionHoleAdmissionFull) {
		t.Fatalf("Admit(over capacity) error = %v", err)
	}
	if _, err := admissions.Admit(502, members[0]); !errors.Is(err, ErrDimensionHoleAdmissionConflict) {
		t.Fatalf("Admit(second portal) error = %v", err)
	}
}

func TestDimensionHoleAdmissionServiceConcurrentAdmissionNeverExceedsCapacity(t *testing.T) {
	definition := model.DimensionHoleCatalog()[0]
	owner := dimensionHoleAdmissionTestPlayer(1, 100, int(definition.MinLevel))
	candidates := make([]*model.Player, 20)
	partyPlayers := make([]*model.Player, 0, len(candidates)+1)
	partyPlayers = append(partyPlayers, owner)
	playersByUID := map[uint32]*model.Player{owner.GetUniqueID(): owner}
	for index := range candidates {
		candidates[index] = dimensionHoleAdmissionTestPlayer(index+2, uint32(200+index), int(definition.MinLevel))
		partyPlayers = append(partyPlayers, candidates[index])
		playersByUID[candidates[index].GetUniqueID()] = candidates[index]
	}
	party := dimensionHoleAdmissionTestParty(owner.GetUniqueID(), partyPlayers...)
	for _, player := range partyPlayers {
		player.Party = party
	}
	instance := dimensionHoleAdmissionTestInstance(t, definition, owner, 501, 1)
	admissions, _ := NewDimensionHoleAdmissionService(
		&dimensionHoleAdmissionInstancesFake{byUID: map[uint32]DimensionHolePortalInstance{501: instance}},
		&dimensionHoleAdmissionPlayersFake{byUID: playersByUID},
	)
	errorsByPlayer := make(chan error, len(candidates))
	var wait sync.WaitGroup
	for _, candidate := range candidates {
		wait.Add(1)
		go func(player *model.Player) {
			defer wait.Done()
			_, err := admissions.Admit(501, player)
			errorsByPlayer <- err
		}(candidate)
	}
	wait.Wait()
	close(errorsByPlayer)
	successes := 0
	for err := range errorsByPlayer {
		if err == nil {
			successes++
			continue
		}
		if !errors.Is(err, ErrDimensionHoleAdmissionFull) {
			t.Fatalf("concurrent Admit() error = %v", err)
		}
	}
	if successes != int(definition.Capacity) {
		t.Fatalf("successful admissions = %d; want %d", successes, definition.Capacity)
	}
	members := admissions.Members(501)
	if len(members) != int(definition.Capacity) || !sort.SliceIsSorted(members, func(i, j int) bool { return members[i] < members[j] }) {
		t.Fatalf("Members(501) = %v", members)
	}
}

func TestDimensionHoleAdmissionServiceRejectsMissingDependencies(t *testing.T) {
	if admissions, err := NewDimensionHoleAdmissionService(nil, nil); admissions != nil || !errors.Is(err, ErrDimensionHoleAdmissionUnavailable) {
		t.Fatalf("NewDimensionHoleAdmissionService(nil) = %#v, %v", admissions, err)
	}
}

func dimensionHoleAdmissionTestPlayer(id int, uid uint32, level int) *model.Player {
	player := dimensionHolePortalTestOwner(id, uid, "player")
	player.CharName = "player-" + string(rune('A'+id))
	player.Level = level
	return player
}

func dimensionHoleAdmissionTestParty(masterUID uint32, members ...*model.Player) *model.Party {
	memberUIDs := make([]uint32, 0, len(members))
	for _, member := range members {
		memberUIDs = append(memberUIDs, member.GetUniqueID())
	}
	return &model.Party{Number: 1, MasterUniqueID: masterUID, Members: memberUIDs, Mutex: &sync.Mutex{}}
}

func dimensionHoleAdmissionTestInstance(t *testing.T, definition model.DimensionHoleDefinition, owner *model.Player, portalUID uint32, layer uint16) DimensionHolePortalInstance {
	t.Helper()
	portal, err := model.NewDimensionHolePortal(definition, owner, dimensionHolePortalTestPosition())
	if err != nil {
		t.Fatal(err)
	}
	portal.SetUniqueID(portalUID)
	scope, err := model.NewGameWorldScope(definition.WorldID, layer)
	if err != nil {
		t.Fatal(err)
	}
	return DimensionHolePortalInstance{
		Portal: portal,
		Lease:  model.DimensionHoleScopeLease{Scope: scope, ReservationKey: portalUID + 1000, ItemRefID: definition.ItemRefID},
	}
}
