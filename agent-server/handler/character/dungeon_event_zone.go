package character

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/pk2"
	"github.com/g3n/engine/math32"
)

const dungeonObjectStringPath = "Data/navmesh/objectstring.ifo"

type dungeonEventZone struct {
	name        string
	source      navmeshv2.RtNavmeshPosition
	destination model.TeleportDestination
	radius      float32
}

type dungeonEventZoneCatalog struct{ byRegion map[int16][]dungeonEventZone }

// DungeonEventZoneLatch prevents a destination portal from immediately
// returning a character. A character is rearmed only after submitting a
// movement target outside every event-zone volume.
type DungeonEventZoneLatch struct {
	catalog *dungeonEventZoneCatalog
	mu      sync.Mutex
	blocked map[uint32]bool
}

type dungeonTeleportPoint struct {
	id       uint32
	name     string
	position navmeshv2.RtNavmeshPosition
	radius   float32
	worldID  int16
}

func LoadDungeonEventZoneCatalog(dataPK2Path string, points map[uint32]model.DungeonTeleportPoint, links map[uint32]uint32) (*dungeonEventZoneCatalog, error) {
	reader := pk2.NewPk2Reader(dataPK2Path)
	reader.IndexArchive()
	defer reader.Close()
	objectStrings, err := reader.ReadFile(dungeonObjectStringPath)
	if err != nil {
		return nil, fmt.Errorf("read dungeon event-zone geometry: %w", err)
	}
	return loadDungeonEventZoneCatalog(points, links, strings.NewReader(string(objectStrings)))
}

func loadDungeonEventZoneCatalog(databasePoints map[uint32]model.DungeonTeleportPoint, links map[uint32]uint32, objectStrings io.Reader) (*dungeonEventZoneCatalog, error) {
	objectText, err := readDungeonCatalogText(objectStrings)
	if err != nil {
		return nil, fmt.Errorf("read dungeon event-zone geometry: %w", err)
	}
	points := make(map[string]dungeonTeleportPoint, len(databasePoints))
	pointsByID := make(map[uint32]dungeonTeleportPoint, len(databasePoints))
	for _, point := range databasePoints {
		name := strings.ToUpper(point.CodeName)
		isJinsiGate := strings.HasPrefix(name, "GATE_JINSI_")
		isDonwhangCaveGate := strings.HasPrefix(name, "GATE_DUNGEON_DH_")
		if !isJinsiGate && !isDonwhangCaveGate {
			continue
		}
		if point.Radius <= 0 || point.X == 0 && point.Y == 0 && point.Z == 0 || isJinsiGate && point.Region >= 0 {
			continue
		}
		position := navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(point.Region), Offset: math32.NewVector3(point.X, point.Y, point.Z)}
		internalPoint := dungeonTeleportPoint{id: point.ID, name: name, position: position, radius: point.Radius, worldID: point.WorldID}
		points[name] = internalPoint
		pointsByID[point.ID] = internalPoint
	}
	catalog := &dungeonEventZoneCatalog{byRegion: make(map[int16][]dungeonEventZone)}
	for _, source := range pointsByID {
		if !strings.HasPrefix(source.name, "GATE_DUNGEON_DH_") {
			continue
		}
		destinationID, ok := links[source.id]
		if !ok {
			continue
		}
		target, ok := pointsByID[destinationID]
		if !ok {
			continue
		}
		region := source.position.Region.ID
		catalog.byRegion[region] = append(catalog.byRegion[region], dungeonEventZone{
			name:        source.name,
			source:      source.position,
			destination: model.TeleportDestination{ID: target.id, Region: target.position.Region.ID, X: target.position.Offset.X, Y: target.position.Offset.Y, Z: target.position.Offset.Z, WorldID: target.worldID},
			radius:      source.radius,
		})
	}
	scanner := bufio.NewScanner(strings.NewReader(objectText))
	for line := 1; scanner.Scan(); line++ {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 9 {
			continue
		}
		name := strings.ToUpper(strings.Trim(fields[8], "\""))
		source, ok := points[name]
		if !ok {
			continue
		}
		destinationID, ok := links[source.id]
		if !ok {
			continue
		}
		target, ok := pointsByID[destinationID]
		if !ok {
			continue
		}
		floor, err := dungeonFloorFromGateName(name)
		if err != nil || floor < 2 || floor > 4 {
			continue
		}
		x, err := parseHexFloat32(fields[4])
		if err != nil {
			return nil, fmt.Errorf("dungeon event-zone line %d X: %w", line, err)
		}
		y, err := parseHexFloat32(fields[5])
		if err != nil {
			return nil, fmt.Errorf("dungeon event-zone line %d Y: %w", line, err)
		}
		z, err := parseHexFloat32(fields[6])
		if err != nil {
			return nil, fmt.Errorf("dungeon event-zone line %d Z: %w", line, err)
		}
		region := int16(-32760 - floor)
		catalog.byRegion[region] = append(catalog.byRegion[region], dungeonEventZone{
			name:        name,
			source:      navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(region), Offset: math32.NewVector3(x, y, z)},
			destination: model.TeleportDestination{ID: target.id, Region: target.position.Region.ID, X: target.position.Offset.X, Y: target.position.Offset.Y, Z: target.position.Offset.Z, WorldID: target.worldID},
			radius:      source.radius,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read dungeon event-zone geometry: %w", err)
	}
	return catalog, nil
}

func readDungeonCatalogText(reader io.Reader) (string, error) {
	contents, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	if len(contents) >= 2 && (contents[0] == 0xff && contents[1] == 0xfe || len(contents) >= 4 && contents[1] == 0 && contents[3] == 0) {
		start := 0
		if contents[0] == 0xff && contents[1] == 0xfe {
			start = 2
		}
		contents = contents[start:]
		words := make([]uint16, len(contents)/2)
		for i := range words {
			words[i] = binary.LittleEndian.Uint16(contents[i*2:])
		}
		return string(utf16.Decode(words)), nil
	}
	return strings.TrimPrefix(string(contents), "\ufeff"), nil
}

func dungeonFloorFromGateName(name string) (int, error) {
	const prefix = "GATE_JINSI_"
	if !strings.HasPrefix(name, prefix) || len(name) < len(prefix)+2 {
		return 0, fmt.Errorf("invalid Jinsi gate name %q", name)
	}
	return strconv.Atoi(name[len(prefix) : len(prefix)+2])
}

func parseHexFloat32(value string) (float32, error) {
	bits, err := strconv.ParseUint(strings.TrimPrefix(value, "0x"), 16, 32)
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(uint32(bits)), nil
}

func NewDungeonEventZoneLatch(catalog *dungeonEventZoneCatalog) *DungeonEventZoneLatch {
	return &DungeonEventZoneLatch{catalog: catalog, blocked: make(map[uint32]bool)}
}

func (c *dungeonEventZoneCatalog) ZoneCount(region int16) int {
	if c == nil {
		return 0
	}
	return len(c.byRegion[region])
}

func (l *DungeonEventZoneLatch) DestinationAt(uid uint32, position navmeshv2.RtNavmeshPosition) (model.TeleportDestination, string, bool) {
	if l == nil {
		return model.TeleportDestination{}, "", false
	}
	destination, name, inside := l.catalog.DestinationAt(position)
	l.mu.Lock()
	defer l.mu.Unlock()
	if !inside {
		delete(l.blocked, uid)
		return model.TeleportDestination{}, "", false
	}
	if l.blocked[uid] {
		return model.TeleportDestination{}, "", false
	}
	l.blocked[uid] = true
	return destination, name, true
}

func (c *dungeonEventZoneCatalog) DestinationAt(position navmeshv2.RtNavmeshPosition) (model.TeleportDestination, string, bool) {
	if c == nil || position.Offset == nil {
		return model.TeleportDestination{}, "", false
	}
	for _, zone := range c.byRegion[position.Region.ID] {
		if navmeshv2.HorizontalDistance(position, zone.source) <= zone.radius {
			return zone.destination, zone.name, true
		}
	}
	return model.TeleportDestination{}, "", false
}
