package character

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"

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
	data := "1\t58\tGATE_JINSI_02x01_03\t0\tSN_ZONE_26002\t-32762\t-10468\t0\t5596\t70\t0\t0\t1\n" +
		"1\t126\tGATE_JINSI_03x01\t0\tSN_ZONE_26003\t-32763\t352\t72\t78\t70\t0\t0\t1\n" +
		"1\t59\tGATE_JINSI_02x01_09\t0\tSN_ZONE_26002\t-32762\t-13674\t0\t5596\t70\t0\t0\t1\n" +
		"1\t62\tGATE_JINSI_02x02_09\t0\tSN_ZONE_26002\t-32762\t-8259\t0\t5592\t70\t0\t0\t1\n"
	geometry := "JMXVOBJI1000\n1\n0xffff0013 0x00000000 6 128 0xc65781b0 0xbe193728 0x45aee26e 0xbfc90fdb \"GATE_JINSI_02X01_03\"\n"
	catalog, err := loadDungeonEventZoneCatalog(strings.NewReader(data), strings.NewReader(geometry))
	if err != nil {
		t.Fatal(err)
	}
	x, _ := parseHexFloat32("0xc65781b0")
	y, _ := parseHexFloat32("0xbe193728")
	z, _ := parseHexFloat32("0x45aee26e")
	destination, name, ok := catalog.DestinationAt(navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(-32762), Offset: math32.NewVector3(x, y, z)})
	if !ok || name != "GATE_JINSI_02X01_03" || destination.Region != -32763 || destination.X != 352 || destination.Z != 78 {
		t.Fatalf("destination=%+v name=%q ok=%v", destination, name, ok)
	}
	if _, _, ok = catalog.DestinationAt(navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(-32762), Offset: math32.NewVector3(-12000, 0, 5596)}); ok {
		t.Fatal("non-portal position activated")
	}
}

func TestDungeonEventZoneCatalogLoadsFloorThreeGeometry(t *testing.T) {
	data := "1\t127\tGATE_JINSI_03x02\t0\tSN_ZONE\t-32763\t19140\t549\t15784\t70\t0\t0\t1\n" +
		"1\t132\tGATE_JINSI_04x02\t0\tSN_ZONE\t-32764\t18267\t-98\t14340\t70\t0\t0\t1\n"
	geometry := "0xffff000e 0x00000000 5 128 0x4697a71e 0x440971fa 0x4674ce3e 0x40490fda \"GATE_JINSI_03X02\"\n"
	catalog, err := loadDungeonEventZoneCatalog(strings.NewReader(data), strings.NewReader(geometry))
	if err != nil {
		t.Fatal(err)
	}
	x, _ := parseHexFloat32("0x4697a71e")
	y, _ := parseHexFloat32("0x440971fa")
	z, _ := parseHexFloat32("0x4674ce3e")
	destination, name, ok := catalog.DestinationAt(navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(-32763), Offset: math32.NewVector3(x, y, z)})
	if !ok || name != "GATE_JINSI_03X02" || destination.Region != -32763 || destination.X != 19140 || destination.Z != 15784 {
		t.Fatalf("destination=%+v name=%q ok=%v", destination, name, ok)
	}
}

func TestDungeonEventZoneCatalogDoesNotPairDuplicateDestinations(t *testing.T) {
	data := "1\t109\tGATE_JINSI_02x13_09-1\t0\tSN_ZONE\t-32762\t-1593\t0\t-5819\t70\t0\t0\t1\n" +
		"1\t110\tGATE_JINSI_02x13_09-2\t0\tSN_ZONE\t-32762\t-1567\t0\t-5817\t70\t0\t0\t1\n"
	catalog, err := loadDungeonEventZoneCatalog(strings.NewReader(data), strings.NewReader("JMXVOBJI1000\n0\n"))
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
