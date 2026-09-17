package character

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
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

func LoadDungeonEventZoneCatalog(teleportPath, dataPK2Path string) (*dungeonEventZoneCatalog, error) {
	file, err := os.Open(teleportPath)
	if err != nil {
		return nil, fmt.Errorf("open dungeon teleport catalog: %w", err)
	}
	defer file.Close()
	reader := pk2.NewPk2Reader(dataPK2Path)
	reader.IndexArchive()
	defer reader.Close()
	objectStrings, err := reader.ReadFile(dungeonObjectStringPath)
	if err != nil {
		return nil, fmt.Errorf("read dungeon event-zone geometry: %w", err)
	}
	return loadDungeonEventZoneCatalog(file, strings.NewReader(string(objectStrings)))
}

func loadDungeonEventZoneCatalog(teleports, objectStrings io.Reader) (*dungeonEventZoneCatalog, error) {
	teleportText, err := readDungeonCatalogText(teleports)
	if err != nil {
		return nil, fmt.Errorf("read dungeon teleport catalog: %w", err)
	}
	objectText, err := readDungeonCatalogText(objectStrings)
	if err != nil {
		return nil, fmt.Errorf("read dungeon event-zone geometry: %w", err)
	}
	points := make(map[string]dungeonTeleportPoint)
	scanner := bufio.NewScanner(strings.NewReader(teleportText))
	for line := 1; scanner.Scan(); line++ {
		fields := strings.Split(strings.TrimSuffix(scanner.Text(), "\r"), "\t")
		if len(fields) < 13 || fields[0] != "1" || !strings.HasPrefix(fields[2], "GATE_JINSI_") {
			continue
		}
		parse := func(index int, bits int) (int64, error) { return strconv.ParseInt(fields[index], 10, bits) }
		id, e := parse(1, 32)
		if e != nil {
			return nil, fmt.Errorf("dungeon teleport line %d ID: %w", line, e)
		}
		region, e := parse(5, 16)
		if e != nil {
			return nil, fmt.Errorf("dungeon teleport line %d region: %w", line, e)
		}
		x, e := parse(6, 16)
		if e != nil {
			return nil, fmt.Errorf("dungeon teleport line %d X: %w", line, e)
		}
		y, e := parse(7, 16)
		if e != nil {
			return nil, fmt.Errorf("dungeon teleport line %d Y: %w", line, e)
		}
		z, e := parse(8, 16)
		if e != nil {
			return nil, fmt.Errorf("dungeon teleport line %d Z: %w", line, e)
		}
		radius, e := parse(9, 16)
		if e != nil {
			return nil, fmt.Errorf("dungeon teleport line %d radius: %w", line, e)
		}
		world, e := parse(12, 16)
		if e != nil {
			return nil, fmt.Errorf("dungeon teleport line %d world: %w", line, e)
		}
		if region >= 0 || radius <= 0 || x == 0 && y == 0 && z == 0 {
			continue
		}
		position := navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(int16(region)), Offset: math32.NewVector3(float32(x), float32(y), float32(z))}
		name := strings.ToUpper(fields[2])
		points[name] = dungeonTeleportPoint{id: uint32(id), name: name, position: position, radius: float32(radius), worldID: int16(world)}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read dungeon teleport catalog: %w", err)
	}
	catalog := &dungeonEventZoneCatalog{byRegion: make(map[int16][]dungeonEventZone)}
	scanner = bufio.NewScanner(strings.NewReader(objectText))
	for line := 1; scanner.Scan(); line++ {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 9 {
			continue
		}
		name := strings.ToUpper(strings.Trim(fields[8], "\""))
		destinationName := dungeonGateDestinationName(name)
		target, ok := points[destinationName]
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
			radius:      target.radius,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read dungeon event-zone geometry: %w", err)
	}
	return catalog, nil
}

func dungeonGateDestinationName(sourceName string) string {
	// The stock Jinsi stair markers name the landing-side gate, while their
	// object-string coordinate identifies the trigger that the character is
	// standing on. These two entrance markers form the level-2/level-3 stair;
	// joining every marker to itself leaves the character in the same room.
	switch sourceName {
	case "GATE_JINSI_02X01_03":
		return "GATE_JINSI_03X01"
	case "GATE_JINSI_03X01":
		return "GATE_JINSI_02X01_03"
	default:
		return sourceName
	}
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
