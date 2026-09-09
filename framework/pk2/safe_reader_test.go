package pk2

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Harvi8/AegisCore/framework/security/blowfish"
)

func TestOpenPk2ReaderRejectsTruncatedAndInvalidHeaders(t *testing.T) {
	t.Run("truncated", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "truncated.pk2")
		if err := os.WriteFile(path, make([]byte, HeaderSize-1), 0o600); err != nil {
			t.Fatal(err)
		}
		reader, err := OpenPk2Reader(path)
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		if err := reader.IndexArchiveSafe(); !errors.Is(err, ErrArchiveHeader) {
			t.Fatalf("expected header error, got %v", err)
		}
	})

	t.Run("invalid encryption flag", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "invalid-flag.pk2")
		header := make([]byte, HeaderSize)
		header[HeaderEncryptedOffset] = 2
		if err := os.WriteFile(path, header, 0o600); err != nil {
			t.Fatal(err)
		}
		reader, err := OpenPk2Reader(path)
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		if err := reader.IndexArchiveSafe(); !errors.Is(err, ErrArchiveHeader) {
			t.Fatalf("expected header error, got %v", err)
		}
	})
}

func TestSafePk2ReaderIndexesAndReadsFile(t *testing.T) {
	path := writeSyntheticPK2(t, nil)
	reader, err := OpenPk2Reader(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := reader.IndexArchiveSafe(); err != nil {
		t.Fatal(err)
	}
	if err := reader.IndexArchiveSafe(); !errors.Is(err, ErrArchiveAlreadyIndex) {
		t.Fatalf("expected already-indexed error, got %v", err)
	}

	root := "fixture" + string(os.PathSeparator)
	data, err := reader.ReadFile(root + "hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, []byte("safe pk2 payload")) {
		t.Fatalf("unexpected payload %q", data)
	}
	data[0] = 'X'
	dataAgain, err := reader.ReadFile(root + "hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if dataAgain[0] != 's' {
		t.Fatalf("cached payload was returned by reference: %q", dataAgain)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadFile(root + "hello.txt"); !errors.Is(err, ErrArchiveClosed) {
		t.Fatalf("expected closed error, got %v", err)
	}
}

func TestSafePk2ReaderAcceptsJoymaxParentDirectoryMarker(t *testing.T) {
	path := writeSyntheticPK2(t, func(block []byte, _ uint64) {
		offset := EntrySize
		block[offset+EntryTypeOffset] = TypeDir
		copy(block[offset+EntryNameOffset:offset+EntryNameOffset+2], "..")
		binary.LittleEndian.PutUint64(block[offset+EntryPositionOffset:offset+EntryPositionOffset+8], HeaderSize)
	})
	reader, err := OpenPk2Reader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if err := reader.IndexArchiveSafe(); err != nil {
		t.Fatalf("IndexArchiveSafe rejected Joymax parent marker: %v", err)
	}
}

func TestSafePk2ReaderRejectsUnsafeEntriesAndChains(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(block []byte, fileSize uint64)
		expectErr error
	}{
		{
			name: "file range",
			mutate: func(block []byte, fileSize uint64) {
				binary.LittleEndian.PutUint64(block[EntryPositionOffset:EntryPositionOffset+8], fileSize+1)
			},
			expectErr: ErrArchiveEntry,
		},
		{
			name: "path traversal",
			mutate: func(block []byte, _ uint64) {
				copy(block[EntryNameOffset:EntryNameOffset+len("..\\escape")], "..\\escape")
			},
			expectErr: ErrArchiveEntry,
		},
		{
			name: "chain cycle",
			mutate: func(block []byte, _ uint64) {
				offset := 19*EntrySize + EntryNextChainOffset
				binary.LittleEndian.PutUint64(block[offset:offset+8], HeaderSize)
			},
			expectErr: ErrArchiveCycle,
		},
		{
			name: "chain out of bounds",
			mutate: func(block []byte, fileSize uint64) {
				offset := 19*EntrySize + EntryNextChainOffset
				binary.LittleEndian.PutUint64(block[offset:offset+8], fileSize)
			},
			expectErr: ErrArchiveEntry,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writeSyntheticPK2(t, test.mutate)
			reader, err := OpenPk2Reader(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reader.Close()
			if err := reader.IndexArchiveSafe(); !errors.Is(err, test.expectErr) {
				t.Fatalf("expected %v, got %v", test.expectErr, err)
			}
		})
	}
}

func TestSafePk2ReaderReportsNotIndexed(t *testing.T) {
	path := writeSyntheticPK2(t, nil)
	reader, err := OpenPk2Reader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if _, err := reader.ReadFile("fixture" + string(os.PathSeparator) + "hello.txt"); !errors.Is(err, ErrArchiveNotIndexed) {
		t.Fatalf("expected not-indexed error, got %v", err)
	}
}

func TestSafePk2ReaderReportsMissingFile(t *testing.T) {
	path := writeSyntheticPK2(t, nil)
	reader, err := OpenPk2Reader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	if err := reader.IndexArchiveSafe(); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadFile("missing.file"); !errors.Is(err, ErrArchiveFileNotFound) {
		t.Fatalf("ReadFile() error = %v, want ErrArchiveFileNotFound", err)
	}
}

func writeSyntheticPK2(t *testing.T, mutate func(block []byte, fileSize uint64)) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.pk2")
	payload := []byte("safe pk2 payload")
	fileSize := uint64(HeaderSize + BlockSize + len(payload))
	header := make([]byte, HeaderSize)
	binary.LittleEndian.PutUint32(header[HeaderVersionOffset:HeaderVersionOffset+4], 1)
	block := make([]byte, BlockSize)
	block[EntryTypeOffset] = TypeFile
	copy(block[EntryNameOffset:EntryNameOffset+len("hello.txt")], "hello.txt")
	binary.LittleEndian.PutUint64(block[EntryPositionOffset:EntryPositionOffset+8], uint64(HeaderSize+BlockSize))
	binary.LittleEndian.PutUint32(block[EntrySizeOffset:EntrySizeOffset+4], uint32(len(payload)))
	if mutate != nil {
		mutate(block, fileSize)
	}
	cipher, err := blowfish.NewCipher(GeneratePk2BlowfishKey())
	if err != nil {
		t.Fatal(err)
	}
	for offset := 0; offset < len(block); offset += blowfish.BlockSize {
		cipher.EncryptRev(block[offset:offset+blowfish.BlockSize], block[offset:offset+blowfish.BlockSize])
	}
	contents := append(append(header, block...), payload...)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
