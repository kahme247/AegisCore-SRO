package character

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/g3n/engine/math32"
)

func TestReadDungeonCatalogTextDecodesUTF16LE(t *testing.T) {
	words := append([]uint16{0xfeff}, utf16.Encode([]rune("1\tGATE_JINSI\r\n"))...)
	encoded := make([]byte, len(words)*2)
	for i, word := range words {
		binary.LittleEndian.PutUint16(encoded[i*2:], word)
	}
	got, err := readDungeonCatalogText(bytes.NewReader(encoded))
	if err != nil || got != "1\tGATE_JINSI\r\n" {
		t.Fatalf("text=%q err=%v", got, err)
	}
}

func TestDungeonEventZoneCatalogJoinsAuthoritativeSourceAndDestination(t *testing.T) {
	points := map[uint32]model.DungeonTeleportPoint{
		58:  {ID: 58, CodeName: "GATE_JINSI_02x01_03", Region: -32762, X: -10468, Z: 5596, Radius: 70, WorldID: 1},
		59:  {ID: 59, CodeName: "GATE_JINSI_02x01_09", Region: -32762, X: -13674, Z: 5596, Radius: 70, WorldID: 1},
		62:  {ID: 62, CodeName: "GATE_JINSI_02x02_09", Region: -32762, X: -8259, Z: 5592, Radius: 70, WorldID: 1},
		126: {ID: 126, CodeName: "GATE_JINSI_03x01", Region: -32763, X: 352, Y: 72, Z: 78, Radius: 70, WorldID: 1},
	}
	geometry := "JMXVOBJI1000\n1\n0xffff0013 0x00000000 6 128 0xc65781b0 0xbe193728 0x45aee26e 0xbfc90fdb \"GATE_JINSI_02X01_03\"\n"
	links := map[uint32]uint32{58: 62}
	catalog, err := loadDungeonEventZoneCatalog(points, links, strings.NewReader(geometry))
	if err != nil {
		t.Fatal(err)
	}
	x, _ := parseHexFloat32("0xc65781b0")
	y, _ := parseHexFloat32("0xbe193728")
	z, _ := parseHexFloat32("0x45aee26e")
	destination, name, ok := catalog.DestinationAt(navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(-32762), Offset: math32.NewVector3(x, y, z)})
	if !ok || name != "GATE_JINSI_02X01_03" || destination.ID != 62 || destination.Region != -32762 || destination.X != -8259 || destination.Z != 5592 {
		t.Fatalf("destination=%+v name=%q ok=%v", destination, name, ok)
	}
	if _, _, ok = catalog.DestinationAt(navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(-32762), Offset: math32.NewVector3(-12000, 0, 5596)}); ok {
		t.Fatal("non-portal position activated")
	}
}

func TestDungeonEventZoneCatalogLoadsDonwhangStoneCaveBothDirections(t *testing.T) {
	points := map[uint32]model.DungeonTeleportPoint{
		10: {ID: 10, CodeName: "GATE_DUNGEON_DH_OUT", Region: -32767, X: 1011, Z: -862, Radius: 90, WorldID: 1},
		11: {ID: 11, CodeName: "GATE_DUNGEON_DH_IN", Region: 27027, X: 1814, Y: 452, Z: 1834, Radius: 150, WorldID: 1},
	}
	catalog, err := loadDungeonEventZoneCatalog(points, map[uint32]uint32{10: 11, 11: 10}, strings.NewReader("JMXVOBJI1000\n0\n"))
	if err != nil {
		t.Fatal(err)
	}

	inside := navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(-32767), Offset: math32.NewVector3(1011, 0, -862)}
	destination, name, ok := catalog.DestinationAt(inside)
	if !ok || name != "GATE_DUNGEON_DH_OUT" || destination.ID != 11 || destination.Region != 27027 || destination.X != 1814 || destination.Y != 452 || destination.Z != 1834 {
		t.Fatalf("inside destination=%+v name=%q ok=%v", destination, name, ok)
	}

	outside := navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(27027), Offset: math32.NewVector3(1814, 452, 1834)}
	destination, name, ok = catalog.DestinationAt(outside)
	if !ok || name != "GATE_DUNGEON_DH_IN" || destination.ID != 10 || destination.Region != -32767 || destination.X != 1011 || destination.Y != 0 || destination.Z != -862 {
		t.Fatalf("outside destination=%+v name=%q ok=%v", destination, name, ok)
	}
}

func TestDungeonEventZoneCatalogLoadsFloorThreeGeometry(t *testing.T) {
	points := map[uint32]model.DungeonTeleportPoint{
		127: {ID: 127, CodeName: "GATE_JINSI_03x02", Region: -32763, X: 19140, Y: 549, Z: 15784, Radius: 70, WorldID: 1},
		132: {ID: 132, CodeName: "GATE_JINSI_04x02", Region: -32764, X: 18267, Y: -98, Z: 14340, Radius: 70, WorldID: 1},
	}
	geometry := "0xffff000e 0x00000000 5 128 0x4697a71e 0x440971fa 0x4674ce3e 0x40490fda \"GATE_JINSI_03X02\"\n"
	links := map[uint32]uint32{127: 132}
	catalog, err := loadDungeonEventZoneCatalog(points, links, strings.NewReader(geometry))
	if err != nil {
		t.Fatal(err)
	}
	x, _ := parseHexFloat32("0x4697a71e")
	y, _ := parseHexFloat32("0x440971fa")
	z, _ := parseHexFloat32("0x4674ce3e")
	destination, name, ok := catalog.DestinationAt(navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(-32763), Offset: math32.NewVector3(x, y, z)})
	if !ok || name != "GATE_JINSI_03X02" || destination.ID != 132 || destination.Region != -32764 || destination.X != 18267 || destination.Z != 14340 {
		t.Fatalf("destination=%+v name=%q ok=%v", destination, name, ok)
	}
}

func TestDungeonEventZoneCatalogDoesNotPairDuplicateDestinations(t *testing.T) {
	points := map[uint32]model.DungeonTeleportPoint{
		109: {ID: 109, CodeName: "GATE_JINSI_02x13_09-1", Region: -32762, X: -1593, Z: -5819, Radius: 70, WorldID: 1},
		110: {ID: 110, CodeName: "GATE_JINSI_02x13_09-2", Region: -32762, X: -1567, Z: -5817, Radius: 70, WorldID: 1},
	}
	catalog, err := loadDungeonEventZoneCatalog(points, nil, strings.NewReader("JMXVOBJI1000\n0\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := catalog.DestinationAt(navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(-32762), Offset: math32.NewVector3(-1593, 0, -5819)}); ok {
		t.Fatal("duplicate destinations were incorrectly linked as a gate")
	}
}

func TestDungeonEventZoneLatchRequiresExitBeforeReactivation(t *testing.T) {
	region := navmeshv2.NewRegionFromInt16(-32762)
	zonePosition := navmeshv2.RtNavmeshPosition{Region: region, Offset: math32.NewVector3(100, 0, 200)}
	catalog := &dungeonEventZoneCatalog{byRegion: map[int16][]dungeonEventZone{
		region.ID: {{name: "gate", source: zonePosition, radius: 70}},
	}}
	latch := NewDungeonEventZoneLatch(catalog)
	if _, _, ok := latch.DestinationAt(7, zonePosition); !ok {
		t.Fatal("first entry did not activate")
	}
	if _, _, ok := latch.DestinationAt(7, zonePosition); ok {
		t.Fatal("portal reactivated before exit")
	}
	outside := navmeshv2.RtNavmeshPosition{Region: region, Offset: math32.NewVector3(500, 0, 500)}
	if _, _, ok := latch.DestinationAt(7, outside); ok {
		t.Fatal("outside position activated")
	}
	if _, _, ok := latch.DestinationAt(7, zonePosition); !ok {
		t.Fatal("portal did not rearm after exit")
	}
}
