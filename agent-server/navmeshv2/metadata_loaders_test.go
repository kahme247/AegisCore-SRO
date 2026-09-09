package navmeshv2

import (
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

func TestLoadMapProjectInfoContentValidatesAndIndexesRegions(t *testing.T) {
	content := validMapProjectInfoContent(2, 1)
	content[SignatureByteLength+DataByteLength] = 0xc0

	info, err := loadMapProjectInfoContent("mapinfo.mfo", content)
	if err != nil {
		t.Fatalf("loadMapProjectInfoContent() error = %v", err)
	}
	if info.ActiveRegionsCount != 2 {
		t.Fatalf("ActiveRegionsCount = %d, want 2", info.ActiveRegionsCount)
	}
	if !info.EnabledRegions[0] || !info.EnabledRegions[1] {
		t.Fatalf("EnabledRegions = %v, want region IDs 0 and 1", info.EnabledRegions)
	}
}

func TestLoadMapProjectInfoContentRejectsMalformedData(t *testing.T) {
	tests := []struct {
		name    string
		content func() []byte
		want    error
	}{
		{
			name:    "truncated file",
			content: func() []byte { return make([]byte, TotalByteLength-1) },
			want:    ErrInvalidNavmeshData,
		},
		{
			name: "wrong signature",
			content: func() []byte {
				content := validMapProjectInfoContent(1, 1)
				copy(content, "JMXVMFO 9999")
				return content
			},
			want: ErrInvalidNavmeshHeader,
		},
		{
			name: "width exceeds bitmap",
			content: func() []byte {
				return validMapProjectInfoContent(RegionsX+1, 1)
			},
			want: ErrInvalidNavmeshData,
		},
		{
			name: "height exceeds bitmap",
			content: func() []byte {
				return validMapProjectInfoContent(1, RegionsY+1)
			},
			want: ErrInvalidNavmeshData,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := loadMapProjectInfoContent("mapinfo.mfo", test.content())
			assertTypedNavmeshLoadError(t, err, test.want)
		})
	}
}

func TestIsEnabledRejectsShortRegionBitmap(t *testing.T) {
	info := MapProjectInfo{MapWidth: 2, MapHeight: 1}
	if IsEnabled(1, 0, info) {
		t.Fatal("IsEnabled() = true with an empty region bitmap")
	}
}

func TestLoadObjectInfoContentParsesValidEntries(t *testing.T) {
	content := strings.Join([]string{
		objectIfoHeader,
		"2",
		"00001\t0x00000000\t\"navmesh\\first.bms\"",
		"00042\t0xffffffff\t\"navmesh\\second.bsr\"",
		"",
	}, "\r\n")

	info, err := loadObjectInfoContent("object.ifo", []byte(content))
	if err != nil {
		t.Fatalf("loadObjectInfoContent() error = %v", err)
	}
	if len(info.Objects) != 2 {
		t.Fatalf("len(Objects) = %d, want 2", len(info.Objects))
	}
	if got := info.Objects[42]; got.Flag != 0xffffffff || got.FilePath != `navmesh\second.bsr` {
		t.Fatalf("Objects[42] = %+v", got)
	}
}

func TestLoadObjectInfoContentRejectsMalformedData(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    error
	}{
		{name: "wrong header", content: "wrong\n0\n", want: ErrInvalidNavmeshHeader},
		{name: "missing count", content: objectIfoHeader, want: ErrInvalidNavmeshData},
		{name: "invalid count", content: objectIfoHeader + "\ninvalid\n", want: ErrInvalidNavmeshData},
		{name: "count beyond lines", content: objectIfoHeader + "\n2\n00001\t0x00000000\t\"one.bms\"\n", want: ErrInvalidNavmeshData},
		{name: "short entry", content: objectIfoHeader + "\n1\nshort\n", want: ErrInvalidNavmeshData},
		{name: "invalid index", content: objectIfoHeader + "\n1\nabcde\t0x00000000\t\"one.bms\"\n", want: ErrInvalidNavmeshData},
		{name: "invalid flag", content: objectIfoHeader + "\n1\n00001\tnot-a-flag\t\"one.bms\"\n", want: ErrInvalidNavmeshData},
		{name: "unquoted path", content: objectIfoHeader + "\n1\n00001\t0x00000000\tone.bms\n", want: ErrInvalidNavmeshData},
		{name: "empty path", content: objectIfoHeader + "\n1\n00001\t0x00000000\t\"\"\n", want: ErrInvalidNavmeshData},
		{name: "duplicate index", content: objectIfoHeader + "\n2\n00001\t0x00000000\t\"one.bms\"\n00001\t0x00000000\t\"two.bms\"\n", want: ErrInvalidNavmeshData},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := loadObjectInfoContent("object.ifo", []byte(test.content))
			assertTypedNavmeshLoadError(t, err, test.want)
		})
	}
}

func TestLoadDungeonInfoContentParsesValidEntries(t *testing.T) {
	content := "header\r\n1\t42\t\"dungeon\\first\"\r\n0\t7\t\"dungeon\\second\"\r\n"
	info, err := loadDungeonInfoContent("dungeoninfo.txt", []byte(content))
	if err != nil {
		t.Fatalf("loadDungeonInfoContent() error = %v", err)
	}
	if len(info.Dungeons) != 2 || info.Dungeons[0].Service != 1 || info.Dungeons[0].ID != 42 {
		t.Fatalf("Dungeons = %+v", info.Dungeons)
	}
}

func TestLoadDungeonInfoContentRejectsMalformedData(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "empty header", content: ""},
		{name: "invalid service", content: "header\n2\t42\t\"dungeon\"\n"},
		{name: "missing id", content: "header\n1\t\t\"dungeon\"\n"},
		{name: "unquoted path", content: "header\n1\t42\tdungeon\n"},
		{name: "integer overflow", content: "header\n1\t999999999999999999999999999999\t\"dungeon\"\n"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := loadDungeonInfoContent("dungeoninfo.txt", []byte(test.content))
			assertTypedNavmeshLoadError(t, err, ErrInvalidNavmeshData)
		})
	}
}

func TestMetadataLoadersRejectNilPK2Reader(t *testing.T) {
	_, mapErr := LoadMapProjectInfo(nil)
	assertTypedNavmeshLoadError(t, mapErr, ErrInvalidNavmeshData)
	_, objectErr := LoadObjectInfo(nil)
	assertTypedNavmeshLoadError(t, objectErr, ErrInvalidNavmeshData)
	_, dungeonErr := LoadDungeonInfo(nil)
	assertTypedNavmeshLoadError(t, dungeonErr, ErrInvalidNavmeshData)
}

func validMapProjectInfoContent(width, height uint16) []byte {
	content := make([]byte, TotalByteLength)
	copy(content, "JMXVMFO 1000")
	binary.LittleEndian.PutUint16(content[SignatureByteLength:], width)
	binary.LittleEndian.PutUint16(content[SignatureByteLength+2:], height)
	return content
}

func assertTypedNavmeshLoadError(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	var loadErr *NavmeshLoadError
	if !errors.As(err, &loadErr) {
		t.Fatalf("error type = %T, want *NavmeshLoadError", err)
	}
}
