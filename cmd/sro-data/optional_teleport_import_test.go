package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"
)

func TestReadOptionalTeleportsPreservesVSROCoordinateOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "refoptionalteleport.txt")
	contents := "1\t19\tname\tSN_ZONE\t25000\t100\t200\t300\t1\t-1\t1\t10\t80\t-1\txxx\t-1\txxx\t-1\txxx\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err := readOptionalTeleports(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].id != 19 || rows[0].region != 25000 || rows[0].x != 100 || rows[0].y != 200 || rows[0].z != 300 || rows[0].minLevel != 10 || rows[0].maxLevel != 80 {
		t.Fatalf("parsed rows = %#v", rows)
	}
}

func TestDecodeOptionalTeleportTextAcceptsUTF16LE(t *testing.T) {
	units := utf16.Encode([]rune("1\t2\tzone\n"))
	data := []byte{0xff, 0xfe}
	for _, unit := range units {
		encoded := make([]byte, 2)
		binary.LittleEndian.PutUint16(encoded, unit)
		data = append(data, encoded...)
	}
	decoded, err := decodeOptionalTeleportText(data)
	if err != nil || decoded != "1\t2\tzone\n" {
		t.Fatalf("decoded UTF-16 = %q, %v", decoded, err)
	}
}
