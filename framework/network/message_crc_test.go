package network

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func TestMessageCRCComputeGoldenPackets(t *testing.T) {
	tests := []struct {
		name   string
		seed   uint32
		buffer []byte
		want   byte
	}{
		{
			name:   "seed 114 first handshake",
			seed:   114,
			buffer: []byte{0x0c, 0x00, 0x00, 0x50, 0xe0, 0xae, 0x14, 0x59, 0xe7, 0x25, 0xba, 0x59, 0x83, 0xf4, 0xb5, 0x14, 0x08, 0x22},
			want:   90,
		},
		{
			name:   "seed 114 second handshake",
			seed:   114,
			buffer: []byte{12, 0, 0, 80, 224, 224, 94, 1, 87, 77, 12, 244, 110, 165, 237, 196, 185, 136},
			want:   75,
		},
		{
			name:   "seed 88 encrypted packet",
			seed:   88,
			buffer: []byte{12, 0, 0, 80, 187, 145, 112, 17, 109, 1, 216, 186, 4, 187, 147, 97, 107, 184},
			want:   248,
		},
		{
			name:   "seed 88 short packet",
			seed:   88,
			buffer: []byte{0, 0, 0, 144, 112, 75},
			want:   21,
		},
		{
			name:   "seed 88 application packet",
			seed:   88,
			buffer: []byte{12, 128, 1, 32, 49, 202, 9, 0, 83, 82, 198, 215, 162, 227, 49, 66, 144, 133},
			want:   58,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			crc := NewMessageCRC(test.seed)
			if got := crc.Compute(test.buffer); got != test.want {
				t.Fatalf("Compute() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestNewMessageCRCShiftsSeedIntoTablePage(t *testing.T) {
	for _, seed := range []uint32{0, 1, 88, 114, 255} {
		if got, want := NewMessageCRC(seed).seed, seed<<8; got != want {
			t.Fatalf("NewMessageCRC(%d).seed = %d, want %d", seed, got, want)
		}
	}
}

func TestGenerateTableMatchesAll65536GoldenEntries(t *testing.T) {
	// The previous fixture checked in all 65,536 uint32 values directly. This
	// digest is over the identical vector sequence encoded little-endian, so it
	// preserves full-table coverage without a 65,000-line generated source file.
	const wantDigest = "c91f698349b0e0aa783ad990067847f865f2da1af63de9ea9d094f165d3a8684"
	table := GenerateTable()
	if got, want := len(table), 256*256; got != want {
		t.Fatalf("GenerateTable() length = %d, want %d", got, want)
	}
	hash := sha256.New()
	var encoded [4]byte
	for _, value := range table {
		binary.LittleEndian.PutUint32(encoded[:], value)
		_, _ = hash.Write(encoded[:])
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != wantDigest {
		t.Fatalf("GenerateTable() SHA-256 = %s, want %s", got, wantDigest)
	}
}
