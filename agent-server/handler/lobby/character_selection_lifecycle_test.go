package lobby

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/framework/utils"
	"github.com/g3n/engine/math32"
)

type lifecycleJoinWorld struct {
	added           *model.Player
	disconnectedUID uint32
	disconnected    string
}

type recoveringCharacterStore struct {
	fakeCharacterStore
	recoveryCalls int
	recoveryID    int
	recoveryOwner uint32
	recoveryShard uint16
}

func (s *recoveringCharacterStore) RecoverDeadCharacterForJoin(_ context.Context, characterID int, userID uint32, shardID uint16, hp, mp int) (bool, error) {
	s.recoveryCalls++
	s.recoveryID = characterID
	s.recoveryOwner = userID
	s.recoveryShard = shardID
	s.character.HP = hp
	s.character.MP = mp
	if s.character.ReturnTeleportID == 20 {
		s.character.Region = 26959
		s.character.PosX = 950
		s.character.PosY = 84
		s.character.PosZ = 1070
	} else {
		s.character.Region = 25000
		s.character.PosX = 969
		s.character.PosY = 0
		s.character.PosZ = 1369
	}
	return true, nil
}

func (w *lifecycleJoinWorld) AddPlayer(player *model.Player) error {
	w.added = player
	player.SetUniqueID(77)
	return nil
}

func (w *lifecycleJoinWorld) PlayerDisconnected(uid uint32, charName string) {
	w.disconnectedUID = uid
	w.disconnected = charName
}

// bootstrapFailConn lets the join response complete and then fails the first
// character bootstrap packet. It exercises cleanup after WorldService has
// allocated a UID without requiring a live world or database fixture.
type bootstrapFailConn struct {
	failAfter int
	writes    int
}

func (c *bootstrapFailConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (c *bootstrapFailConn) Close() error                     { return nil }
func (c *bootstrapFailConn) LocalAddr() net.Addr              { return lifecycleTestAddr("local") }
func (c *bootstrapFailConn) RemoteAddr() net.Addr             { return lifecycleTestAddr("remote") }
func (c *bootstrapFailConn) SetDeadline(time.Time) error      { return nil }
func (c *bootstrapFailConn) SetReadDeadline(time.Time) error  { return nil }
func (c *bootstrapFailConn) SetWriteDeadline(time.Time) error { return nil }

func (c *bootstrapFailConn) Write(payload []byte) (int, error) {
	if c.writes >= c.failAfter {
		return 0, errors.New("intentional bootstrap write failure")
	}
	c.writes++
	return len(payload), nil
}

type lifecycleTestAddr string

func (a lifecycleTestAddr) Network() string { return "test" }
func (a lifecycleTestAddr) String() string  { return string(a) }

func TestCharacterJoinBootstrapFailureRemovesStagedPlayerAndClearsSession(t *testing.T) {
	conn := &bootstrapFailConn{failAfter: 1}
	session := server.NewSession(conn, network.EncodingOptions{}, nil, nil, "join-lifecycle-test")
	defer session.Close()
	session.MarkAuthenticated()
	session.UserContext.UserID = 42
	session.UserContext.ShardID = 1

	character := model.Char{ID: 7, User: 42, Shard: 1, Name: "owned-character", HP: 1, MP: 1}
	store := &fakeCharacterStore{character: character}
	world := &lifecycleJoinWorld{}
	handler := NewCharacterSelectionHandler(store)
	handler.world = world
	handler.loadPlayer = func(char model.Char, owner *server.Session) (*model.Player, error) {
		return &model.Player{
			Session:  owner,
			ID:       char.ID,
			CharName: char.Name,
			SRObject: model.SRObject{RefObjectID: 100, Name: char.Name},
		}, nil
	}

	request := buildCharacterSelectionRequest(t, func(packet *network.Packet) error {
		return packet.WriteString(character.Name)
	})
	if err := handler.handleJoin(context.Background(), session, request); err == nil {
		t.Fatal("join succeeded despite bootstrap write failure")
	}

	if world.added == nil {
		t.Fatal("join never staged the validated player in world")
	}
	if got, want := world.disconnectedUID, uint32(77); got != want {
		t.Fatalf("cleanup disconnected UID = %d, want %d", got, want)
	}
	if got, want := world.disconnected, character.Name; got != want {
		t.Fatalf("cleanup disconnected character = %q, want %q", got, want)
	}
	if got, want := session.LifecyclePhase(), server.SessionLobby; got != want {
		t.Fatalf("bootstrap failure lifecycle = %s, want %s", got, want)
	}
	if session.UserContext.UniqueID != 0 || session.UserContext.CharacterID != 0 || session.UserContext.CharName != "" || session.UserContext.RefObjId != 0 {
		t.Fatalf("bootstrap failure retained selected character context: %+v", session.UserContext)
	}
	if got, want := world.added.GetLifeState(), model.Spawning; got != want {
		t.Fatalf("bootstrap failure changed staged player life state to %d, want %d", got, want)
	}
}

func TestCharacterJoinKeepsPlayerSpawningUntilGameReady(t *testing.T) {
	conn := &bootstrapFailConn{failAfter: 100}
	session := server.NewSession(conn, network.EncodingOptions{}, nil, nil, "join-lifecycle-success-test")
	defer session.Close()
	session.MarkAuthenticated()
	session.UserContext.UserID = 42
	session.UserContext.ShardID = 1

	character := model.Char{ID: 7, User: 42, Shard: 1, Name: "owned-character", HP: 1, MP: 1}
	store := &fakeCharacterStore{character: character}
	world := &lifecycleJoinWorld{}
	handler := NewCharacterSelectionHandler(store)
	handler.world = world
	handler.loadPlayer = func(char model.Char, owner *server.Session) (*model.Player, error) {
		return &model.Player{
			Session:   owner,
			ID:        char.ID,
			CharName:  char.Name,
			Inventory: model.Inventory{Items: map[byte]model.Item{}},
			SRObject: model.SRObject{
				RefObjectID: 100,
				Name:        char.Name,
				RtNavmeshPosition: navmeshv2.RtNavmeshPosition{
					Offset: math32.NewVector3(0, 0, 0),
				},
			},
		}, nil
	}

	request := buildCharacterSelectionRequest(t, func(packet *network.Packet) error {
		return packet.WriteString(character.Name)
	})
	if err := handler.handleJoin(context.Background(), session, request); err != nil {
		t.Fatalf("validated character join failed: %v", err)
	}

	if world.added == nil {
		t.Fatal("join did not stage the validated player in world")
	}
	if world.disconnectedUID != 0 {
		t.Fatalf("successful bootstrap removed staged player UID %d", world.disconnectedUID)
	}
	if got, want := session.LifecyclePhase(), server.SessionCharacterLoading; got != want {
		t.Fatalf("successful bootstrap lifecycle = %s, want %s", got, want)
	}
	if got, want := world.added.GetLifeState(), model.Spawning; got != want {
		t.Fatalf("successful bootstrap life state = %d, want %d until GameReady", got, want)
	}
	if got, want := session.UserContext.UniqueID, uint32(77); got != want {
		t.Fatalf("selected character UID = %d, want %d", got, want)
	}
	if got, want := session.UserContext.CharacterID, character.ID; got != want {
		t.Fatalf("selected character ID = %d, want %d", got, want)
	}
	if got, want := session.UserContext.CharName, character.Name; got != want {
		t.Fatalf("selected character name = %q, want %q", got, want)
	}
}

func TestCharacterJoinRecoversDeadCharacterBeforePlayerConstruction(t *testing.T) {
	conn := &bootstrapFailConn{failAfter: 100}
	session := server.NewSession(conn, network.EncodingOptions{}, nil, nil, "dead-join-recovery-test")
	defer session.Close()
	session.MarkAuthenticated()
	session.UserContext.UserID = 42
	session.UserContext.ShardID = 1

	character := model.Char{
		ID: 7, User: 42, Shard: 1, Name: "dead-character",
		Level: 10, Str: 29, Int: 29, HP: 0, MP: 3, ReturnTeleportID: 20,
	}
	store := &recoveringCharacterStore{fakeCharacterStore: fakeCharacterStore{character: character}}
	world := &lifecycleJoinWorld{}
	handler := NewCharacterSelectionHandler(store)
	handler.world = world
	var loaded model.Char
	handler.loadPlayer = func(char model.Char, owner *server.Session) (*model.Player, error) {
		loaded = char
		return &model.Player{
			Session:   owner,
			ID:        char.ID,
			CharName:  char.Name,
			Inventory: model.Inventory{Items: map[byte]model.Item{}},
			SRObject: model.SRObject{
				RefObjectID: 14875,
				Name:        char.Name,
				RtNavmeshPosition: navmeshv2.RtNavmeshPosition{
					Offset: math32.NewVector3(char.PosX, char.PosY, char.PosZ),
				},
			},
		}, nil
	}

	request := buildCharacterSelectionRequest(t, func(packet *network.Packet) error {
		return packet.WriteString(character.Name)
	})
	if err := handler.handleJoin(context.Background(), session, request); err != nil {
		t.Fatalf("dead character join failed: %v", err)
	}

	if got, want := store.recoveryCalls, 1; got != want {
		t.Fatalf("recovery calls = %d, want %d", got, want)
	}
	if store.recoveryID != character.ID || store.recoveryOwner != 42 || store.recoveryShard != 1 {
		t.Fatalf("recovery ownership = (%d, %d, %d), want (%d, 42, 1)", store.recoveryID, store.recoveryOwner, store.recoveryShard, character.ID)
	}
	if got, want := store.characterCalls, 2; got != want {
		t.Fatalf("character reload calls = %d, want %d", got, want)
	}
	if got, want := loaded.HP, utils.BaseHPOrMP(character.Level, character.Str); got != want {
		t.Fatalf("loaded HP = %d, want %d", got, want)
	}
	if got, want := loaded.MP, utils.BaseHPOrMP(character.Level, character.Int); got != want {
		t.Fatalf("loaded MP = %d, want %d", got, want)
	}
	if loaded.Region != 26959 || loaded.PosX != 950 || loaded.PosY != 84 || loaded.PosZ != 1070 {
		t.Fatalf("loaded return position = region %d (%v, %v, %v), want Constantinople", loaded.Region, loaded.PosX, loaded.PosY, loaded.PosZ)
	}
}
