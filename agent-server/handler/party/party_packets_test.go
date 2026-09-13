package party

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/g3n/engine/math32"
)

type partyInstanceExitRecorder struct{ ids []uint32 }

func (r *partyInstanceExitRecorder) SubmitPartyQuestInstanceExit(_ context.Context, player *model.Player) error {
	r.ids = append(r.ids, player.GetUniqueID())
	return nil
}

func TestPartyRemovalPublishesNativeInstanceExitScope(t *testing.T) {
	first := &model.Player{SRObject: model.SRObject{UniqueID: 10}}
	second := &model.Player{SRObject: model.SRObject{UniqueID: 20}}
	recorder := &partyInstanceExitRecorder{}
	handler := &PartyHandler{instanceExit: recorder}
	if err := publishRemoval(context.Background(), handler, service.PartyTransition{Target: first, AllBefore: []*model.Player{first, second}}); err != nil {
		t.Fatal(err)
	}
	if len(recorder.ids) != 1 || recorder.ids[0] != 10 {
		t.Fatalf("single removal exits=%v", recorder.ids)
	}
	recorder.ids = nil
	if err := publishRemoval(context.Background(), handler, service.PartyTransition{Target: first, AllBefore: []*model.Player{first, second}, Dismissed: true}); err != nil {
		t.Fatal(err)
	}
	if len(recorder.ids) != 2 || recorder.ids[0] != 10 || recorder.ids[1] != 20 {
		t.Fatalf("dismiss exits=%v", recorder.ids)
	}
}

func TestWritePartyPositionPublishesInstanceWorldAndLayer(t *testing.T) {
	scope, err := model.NewGameWorldScope(41, 3)
	if err != nil {
		t.Fatal(err)
	}
	packet := network.EmptyPacket()
	writePartyPosition(&packet, navmeshv2.RtNavmeshPosition{
		Region: navmeshv2.NewRegionFromInt16(25000),
		Offset: math32.NewVector3(10, 20, 30),
	}, scope)
	reader := newPartyPacketReader(t, packet.Payload())
	if region, x, z, y, worldID, layerID := reader.uint16(), reader.uint16(), reader.uint16(), reader.uint16(), reader.uint16(), reader.uint16(); region != 25000 || x != 10 || z != 30 || y != 20 || worldID != 41 || layerID != 3 {
		t.Fatalf("party position = region=%d xyz=%d/%d/%d world/layer=%d/%d", region, x, y, z, worldID, layerID)
	}
	reader.done()
}

type partyPacketReader struct {
	t       *testing.T
	payload []byte
	offset  int
}

func newPartyPacketReader(t *testing.T, payload []byte) *partyPacketReader {
	t.Helper()
	return &partyPacketReader{t: t, payload: payload}
}

func (r *partyPacketReader) take(size int) []byte {
	r.t.Helper()
	if size < 0 || r.offset+size > len(r.payload) {
		r.t.Fatalf("party packet truncated at byte %d: need %d bytes, payload is %d bytes", r.offset, size, len(r.payload))
	}
	value := r.payload[r.offset : r.offset+size]
	r.offset += size
	return value
}

func (r *partyPacketReader) byte() byte     { return r.take(1)[0] }
func (r *partyPacketReader) uint16() uint16 { return binary.LittleEndian.Uint16(r.take(2)) }
func (r *partyPacketReader) uint32() uint32 { return binary.LittleEndian.Uint32(r.take(4)) }

func (r *partyPacketReader) string() string {
	size := int(r.uint16())
	return string(r.take(size))
}

func (r *partyPacketReader) done() {
	r.t.Helper()
	if r.offset != len(r.payload) {
		r.t.Fatalf("party packet has %d trailing bytes at offset %d: % x", len(r.payload)-r.offset, r.offset, r.payload[r.offset:])
	}
}

type decodedPartyMember struct {
	uid                uint32
	name               string
	modelID            uint32
	level, vitals      byte
	region             uint16
	x, z, y            uint16
	worldID, layerID   uint16
	guild              string
	marker             byte
	primary, secondary uint32
}

func decodePartyMember(t *testing.T, r *partyPacketReader) decodedPartyMember {
	t.Helper()
	if splitter := r.byte(); splitter != 0xFF {
		t.Fatalf("member splitter = 0x%02X, want 0xFF", splitter)
	}
	return decodedPartyMember{
		uid: r.uint32(), name: r.string(), modelID: r.uint32(), level: r.byte(), vitals: r.byte(),
		region: r.uint16(), x: r.uint16(), z: r.uint16(), y: r.uint16(),
		worldID: r.uint16(), layerID: r.uint16(), guild: r.string(), marker: r.byte(),
		primary: r.uint32(), secondary: r.uint32(),
	}
}

func testPartyPlayer(uid, modelID uint32, name string, level int, hp, mp, strength, intelligence int, region int16, x, y, z float32, masteries map[uint32]byte) *model.Player {
	return &model.Player{
		SRObject: model.SRObject{
			UniqueID: uid, RefObjectID: modelID,
			RtNavmeshPosition: navmeshv2.RtNavmeshPosition{
				Region: navmeshv2.NewRegionFromInt16(region),
				Offset: math32.NewVector3(x, y, z),
			},
		},
		CharName: name, Level: level,
		BaseStats: model.BaseStats{HP: hp, MP: mp, Str: strength, Int: intelligence},
		Masteries: masteries,
	}
}

func assertPartyMember(t *testing.T, got decodedPartyMember, uid, modelID uint32, name string, level, vitals byte, region, x, z, y uint16, primary, secondary uint32) {
	t.Helper()
	if got.uid != uid || got.name != name || got.modelID != modelID || got.level != level || got.vitals != vitals ||
		got.region != region || got.x != x || got.z != z || got.y != y || got.worldID != 1 || got.layerID != 1 ||
		got.guild != "" || got.marker != 4 || got.primary != primary || got.secondary != secondary {
		t.Fatalf("decoded member mismatch:\n got  %+v\n want UID=%08X name=%q model=%08X level=%d vitals=%02X region=%04X X/Z/Y=%d/%d/%d world/layer=1/1 guild=empty marker=4 masteries=%08X/%08X",
			got, uid, name, modelID, level, vitals, region, x, z, y, primary, secondary)
	}
}

func TestBuildInvitationVSROLayout(t *testing.T) {
	for _, test := range []struct {
		name, kind  string
		partyNumber uint32
		wantKind    byte
	}{
		{name: "new party", kind: "new", partyNumber: 0, wantKind: 2},
		{name: "existing party", kind: "existing", partyNumber: 91, wantKind: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			packet := buildInvitation(service.PartyInvitation{InviterUniqueID: 0x11223344, PartyNumber: test.partyNumber, Settings: model.ExpDistItemShare})
			if packet.MessageID != opcode.PartyInvitation {
				t.Fatalf("opcode = 0x%04X, want 0x%04X", packet.MessageID, opcode.PartyInvitation)
			}
			r := newPartyPacketReader(t, packet.Payload())
			if kind, inviter, settings := r.byte(), r.uint32(), r.byte(); kind != test.wantKind || inviter != 0x11223344 || settings != 6 {
				t.Fatalf("%s invitation = kind %d inviter %08X settings %d", test.kind, kind, inviter, settings)
			}
			r.done()
		})
	}
}

func TestBuildPartySnapshotVSROLayoutWithTwoMembers(t *testing.T) {
	first := testPartyPlayer(0x01020304, 1907, "Leader", 1, 70, 60, 10, 20, 0x1234, 101, -303, 202, map[uint32]byte{257: 40, 258: 60, 259: 60})
	second := testPartyPlayer(0xA1A2A3A4, 1920, "Member", 1, 0, 500, 10, 20, 0x2345, 404, 606, 505, map[uint32]byte{300: 20})
	packet := buildPartySnapshot(service.PartySnapshot{
		Number: 77, MasterUniqueID: first.GetUniqueID(), Settings: model.ExpShareItemShare,
		Members: []*model.Player{first, second},
	}, false)
	if packet.MessageID != opcode.PartyCreate {
		t.Fatalf("opcode = 0x%04X, want 0x%04X", packet.MessageID, opcode.PartyCreate)
	}
	r := newPartyPacketReader(t, packet.Payload())
	if splitter, master, settings, count := r.byte(), r.uint32(), r.byte(), r.byte(); splitter != 0xFF || master != 0x01020304 || settings != 7 || count != 2 {
		t.Fatalf("snapshot header = splitter %02X master %08X settings %d count %d", splitter, master, settings, count)
	}
	// Level 1 maxima are Str*10 and Int*10: 70/100 => 7; 60/200 => 3, packed MP:HP as 0x37.
	assertPartyMember(t, decodePartyMember(t, r), 0x01020304, 1907, "Leader", 1, 0x37, 0x1234, 101, 202, 0xFED1, 258, 259)
	// Zero HP packs as 0 and over-max MP is clamped to 10, producing 0xA0.
	assertPartyMember(t, decodePartyMember(t, r), 0xA1A2A3A4, 1920, "Member", 1, 0xA0, 0x2345, 404, 505, 606, 300, 0)
	r.done()
}

func TestBuildPartyJoinVSROLayout(t *testing.T) {
	member := testPartyPlayer(0x55667788, 14875, "Joined", 1, 90, 20, 10, 20, 0x3456, 701, 903, 802, map[uint32]byte{400: 10, 401: 11})
	packet := buildPartyJoin(member)
	if packet.MessageID != opcode.PartyUpdateResponse {
		t.Fatalf("opcode = 0x%04X, want 0x%04X", packet.MessageID, opcode.PartyUpdateResponse)
	}
	r := newPartyPacketReader(t, packet.Payload())
	if action := r.byte(); action != 2 {
		t.Fatalf("join action = %d, want 2", action)
	}
	assertPartyMember(t, decodePartyMember(t, r), 0x55667788, 14875, "Joined", 1, 0x19, 0x3456, 701, 802, 903, 401, 400)
	r.done()
}

func TestPartyUpdateControlPacketVSROLayouts(t *testing.T) {
	leave := buildPartyLeave(0x11223344, 4)
	dismiss := buildPartyDismiss()
	leader := buildPartyLeader(0xA1B2C3D4)
	tests := []struct {
		name   string
		opcode uint16
		got    []byte
		want   []byte
	}{
		{name: "leave", opcode: leave.MessageID, got: leave.Payload(), want: []byte{3, 0x44, 0x33, 0x22, 0x11, 4}},
		{name: "dismiss", opcode: dismiss.MessageID, got: dismiss.Payload(), want: []byte{1}},
		{name: "leader", opcode: leader.MessageID, got: leader.Payload(), want: []byte{9, 0xD4, 0xC3, 0xB2, 0xA1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.opcode != opcode.PartyUpdateResponse || string(test.got) != string(test.want) {
				t.Fatalf("packet = opcode 0x%04X payload % X, want opcode 0x%04X payload % X", test.opcode, test.got, opcode.PartyUpdateResponse, test.want)
			}
		})
	}
}

func TestPackPartyVitals(t *testing.T) {
	for _, test := range []struct {
		hp, mp, maxHP, maxMP int
		want                 byte
	}{
		{hp: 70, mp: 60, maxHP: 100, maxMP: 200, want: 0x37},
		{hp: 0, mp: 500, maxHP: 100, maxMP: 200, want: 0xA0},
		{hp: 500, mp: -1, maxHP: 100, maxMP: 200, want: 0x0A},
		{hp: 1, mp: 1, maxHP: 0, maxMP: 0, want: 0x00},
	} {
		t.Run(fmt.Sprintf("%02X", test.want), func(t *testing.T) {
			if got := packPartyVitals(test.hp, test.mp, test.maxHP, test.maxMP); got != test.want {
				t.Fatalf("packPartyVitals(%d, %d, %d, %d) = 0x%02X, want 0x%02X", test.hp, test.mp, test.maxHP, test.maxMP, got, test.want)
			}
		})
	}
}
