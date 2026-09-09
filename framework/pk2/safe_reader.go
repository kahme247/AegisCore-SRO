package pk2

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/framework/security/blowfish"
)

const (
	safeMaxDirectoryDepth = 128
	safeMaxIndexBlocks    = 1 << 20
	safeMaxIndexEntries   = 20 << 20
	safeMaxEntrySize      = uint64(512 << 20)
	maxUint63             = uint64(1<<63 - 1)
)

var (
	ErrArchiveClosed       = errors.New("pk2 archive is closed")
	ErrArchiveAlreadyIndex = errors.New("pk2 archive is already indexed")
	ErrArchiveHeader       = errors.New("invalid pk2 archive header")
	ErrArchiveBounds       = errors.New("pk2 archive offset or size is out of bounds")
	ErrArchiveCycle        = errors.New("pk2 archive directory or chain cycle")
	ErrArchiveLimit        = errors.New("pk2 archive index limit exceeded")
	ErrArchiveEntry        = errors.New("invalid pk2 archive entry")
	ErrArchiveFileNotFound = errors.New("pk2 archive file not found")
	ErrArchiveNotIndexed   = errors.New("pk2 archive has not been indexed")
)

// OpenPk2Reader opens an archive without panicking. Production loaders should
// use this constructor and IndexArchiveSafe; NewPk2Reader remains as a legacy
// compatibility API for the standalone viewer tools.
func OpenPk2Reader(filename string) (*Pk2Reader, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("open pk2 archive: %w", err)
	}
	cipher, err := blowfish.NewCipher(GeneratePk2BlowfishKey())
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("create pk2 cipher: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("stat pk2 archive: %w", err)
	}
	fileName := filepath.Base(filename)
	fileExt := filepath.Ext(fileName)
	if fileExt != "" {
		fileName = strings.TrimSuffix(fileName, fileExt)
	}
	return &Pk2Reader{
		file:           file,
		cipher:         cipher,
		Directory:      Directory{Name: fileName},
		CachingEnabled: true,
		FileCache:      make(map[string][]byte),
		state:          &pk2ReaderState{},
		cacheMutex:     &sync.RWMutex{},
		fileSize:       info.Size(),
	}, nil
}

// Close releases the archive handle. It is idempotent so shutdown paths can
// safely call it after a failed index or a successful load.
func (r *Pk2Reader) Close() error {
	if r == nil || r.file == nil {
		return nil
	}
	state := r.readerState()
	state.Lock()
	defer state.Unlock()
	if state.closed {
		return nil
	}
	state.closed = true
	return r.file.Close()
}

// IndexArchiveSafe validates all offsets before allocation or recursion and
// rejects cycles, truncated blocks, unsupported entry types, and excessive
// depth/counts. It uses one fixed 20-entry block per PK2 directory chain.
func (r *Pk2Reader) IndexArchiveSafe() error {
	if r == nil || r.file == nil {
		return ErrArchiveClosed
	}
	state := r.readerState()
	state.Lock()
	defer state.Unlock()
	if state.closed {
		return ErrArchiveClosed
	}
	if r.finishedIndexing {
		return ErrArchiveAlreadyIndex
	}
	if r.cipher == nil {
		return fmt.Errorf("%w: cipher is unavailable", ErrArchiveHeader)
	}
	info, err := r.file.Stat()
	if err != nil {
		return fmt.Errorf("stat pk2 archive: %w", err)
	}
	r.fileSize = info.Size()
	header, err := r.readHeaderSafe()
	if err != nil {
		return err
	}
	if err := r.verifyHeaderSafe(header); err != nil {
		return err
	}

	directory := Directory{Name: r.Directory.Name}
	if directory.Name == "" {
		fileName := filepath.Base(r.file.Name())
		directory.Name = strings.TrimSuffix(fileName, filepath.Ext(fileName))
	}
	visited := make(map[uint64]struct{})
	blockCount := 0
	entryCount := 0
	if err := r.readEntriesSafe(uint64(HeaderSize), &directory, 0, visited, &blockCount, &entryCount); err != nil {
		return err
	}
	directory.buildEntryMap()
	directory.buildDirectoryMap()
	r.Directory = directory
	r.Files = directory.AllFiles()
	r.finishedIndexing = true
	return nil
}

func (r *Pk2Reader) readHeaderSafe() (PackHeader, error) {
	if r.fileSize < HeaderSize {
		return PackHeader{}, fmt.Errorf("%w: archive is %d bytes, header requires %d", ErrArchiveHeader, r.fileSize, HeaderSize)
	}
	buffer := make([]byte, HeaderSize)
	count, err := r.file.ReadAt(buffer, 0)
	if err != nil || count != HeaderSize {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return PackHeader{}, fmt.Errorf("%w: read header: %v", ErrArchiveHeader, err)
	}
	encryptionFlag := buffer[HeaderEncryptedOffset]
	if encryptionFlag != 0 && encryptionFlag != 1 {
		return PackHeader{}, fmt.Errorf("%w: encryption flag is invalid", ErrArchiveHeader)
	}
	encrypted := encryptionFlag == 1
	return PackHeader{
		Header:    string(buffer[HeaderHeaderOffset:HeaderVersionOffset]),
		Version:   binary.LittleEndian.Uint32(buffer[HeaderVersionOffset:HeaderEncryptedOffset]),
		Encrypted: encrypted,
		Checksum:  append([]byte(nil), buffer[HeaderChecksumOffset:HeaderReservedOffset]...),
		Reserverd: append([]byte(nil), buffer[HeaderReservedOffset:HeaderSize]...),
	}, nil
}

func (r *Pk2Reader) verifyHeaderSafe(header PackHeader) error {
	if !header.Encrypted {
		return nil
	}
	if len(header.Checksum) < 3 {
		return fmt.Errorf("%w: encrypted checksum is truncated", ErrArchiveHeader)
	}
	encodedChecksum := []byte(checksum)
	r.cipher.EncryptRev(encodedChecksum, []byte(checksum))
	for index := 0; index < 3; index++ {
		if encodedChecksum[index] != header.Checksum[index] {
			return fmt.Errorf("%w: encrypted checksum does not match", ErrArchiveHeader)
		}
	}
	return nil
}

func (r *Pk2Reader) readEntriesSafe(position uint64, directory *Directory, depth int, visited map[uint64]struct{}, blockCount, entryCount *int) error {
	if directory == nil {
		return fmt.Errorf("%w: directory is nil", ErrArchiveEntry)
	}
	if depth > safeMaxDirectoryDepth {
		return fmt.Errorf("%w: directory depth %d exceeds %d", ErrArchiveLimit, depth, safeMaxDirectoryDepth)
	}
	if position < HeaderSize || position > maxUint63 || r.fileSize < int64(BlockSize) || position > uint64(r.fileSize-int64(BlockSize)) {
		return fmt.Errorf("%w: directory block at %d", ErrArchiveBounds, position)
	}
	if _, exists := visited[position]; exists {
		return fmt.Errorf("%w: directory block at %d", ErrArchiveCycle, position)
	}
	visited[position] = struct{}{}
	*blockCount++
	if *blockCount > safeMaxIndexBlocks {
		return fmt.Errorf("%w: more than %d blocks", ErrArchiveLimit, safeMaxIndexBlocks)
	}

	buffer := make([]byte, BlockSize)
	count, err := r.file.ReadAt(buffer, int64(position))
	if err != nil || count != BlockSize {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return fmt.Errorf("%w: read block at %d: %v", ErrArchiveBounds, position, err)
	}
	if err := r.decryptBufferSafe(buffer); err != nil {
		return err
	}

	entries := make([]PackFileEntry, 0, BlockSize/EntrySize)
	for index := 0; index < BlockSize/EntrySize; index++ {
		if *entryCount >= safeMaxIndexEntries {
			return fmt.Errorf("%w: more than %d entries", ErrArchiveLimit, safeMaxIndexEntries)
		}
		offset := index * EntrySize
		nameBytes := buffer[offset+EntryNameOffset : offset+EntryAccessTimeOffset]
		if nul := bytes.IndexByte(nameBytes, 0); nul >= 0 {
			nameBytes = nameBytes[:nul]
		}
		entry := PackFileEntry{
			Type:       buffer[offset+EntryTypeOffset],
			Name:       strings.ReplaceAll(string(nameBytes), "\\", string(os.PathSeparator)),
			AccessTime: safeEntryTime(binary.LittleEndian.Uint64(buffer[offset+EntryAccessTimeOffset : offset+EntryCreateTimeOffset])),
			CreateTime: safeEntryTime(binary.LittleEndian.Uint64(buffer[offset+EntryCreateTimeOffset : offset+EntryModifyTimeOffset])),
			ModifyTime: safeEntryTime(binary.LittleEndian.Uint64(buffer[offset+EntryModifyTimeOffset : offset+EntryPositionOffset])),
			Position:   binary.LittleEndian.Uint64(buffer[offset+EntryPositionOffset : offset+EntrySizeOffset]),
			Size:       binary.LittleEndian.Uint32(buffer[offset+EntrySizeOffset : offset+EntryNextChainOffset]),
			NextChain:  binary.LittleEndian.Uint64(buffer[offset+EntryNextChainOffset : offset+EntryPaddingOffset]),
			Padding:    append([]byte(nil), buffer[offset+EntryPaddingOffset:offset+EntrySize]...),
		}
		if err := r.validateEntrySafe(entry); err != nil {
			return fmt.Errorf("%w: entry %d in %s: %v", ErrArchiveEntry, index, directory.Name, err)
		}
		entries = append(entries, entry)
		*entryCount++
	}

	if nextChain := entries[len(entries)-1].NextChain; nextChain != 0 {
		if err := r.readEntriesSafe(nextChain, directory, depth, visited, blockCount, entryCount); err != nil {
			return err
		}
	}
	directory.Entries = append(directory.Entries, entries...)
	if directory.Directories == nil {
		directory.Directories = make([]Directory, 0)
	}
	for _, entry := range entries {
		if entry.Type != TypeDir || strings.HasPrefix(entry.Name, ".") {
			continue
		}
		child := Directory{
			Name:    directory.Name + string(os.PathSeparator) + entry.Name,
			Entries: make([]PackFileEntry, 0),
			Files:   make([]PackFileEntry, 0),
		}
		if err := r.readEntriesSafe(entry.Position, &child, depth+1, visited, blockCount, entryCount); err != nil {
			return err
		}
		directory.Directories = append(directory.Directories, child)
	}
	return nil
}

func (r *Pk2Reader) validateEntrySafe(entry PackFileEntry) error {
	if entry.Type != TypeEmpty && entry.Type != TypeDir && entry.Type != TypeFile {
		return fmt.Errorf("unknown entry type %d", entry.Type)
	}
	// Joymax directory blocks contain DOS-style metadata entries named "."
	// and "..". The indexer never traverses names beginning with a dot, so
	// accept those two exact directory markers while continuing to reject any
	// file name or compound path that contains a parent component.
	directoryMarker := entry.Type == TypeDir && (entry.Name == "." || entry.Name == "..")
	if entry.Name != "" && !directoryMarker {
		normalizedName := strings.ReplaceAll(entry.Name, "\\", "/")
		if filepath.IsAbs(entry.Name) || filepath.VolumeName(entry.Name) != "" || strings.HasPrefix(normalizedName, "/") {
			return fmt.Errorf("entry name %q is absolute", entry.Name)
		}
		for _, component := range strings.Split(normalizedName, "/") {
			if component == ".." {
				return fmt.Errorf("entry name %q escapes its directory", entry.Name)
			}
		}
	}
	if entry.Type == TypeFile {
		if uint64(entry.Size) > safeMaxEntrySize {
			return fmt.Errorf("file size %d exceeds %d", entry.Size, safeMaxEntrySize)
		}
		if entry.Position > uint64(r.fileSize) || uint64(entry.Size) > uint64(r.fileSize)-entry.Position {
			return fmt.Errorf("file range starts at %d with size %d in archive of %d bytes", entry.Position, entry.Size, r.fileSize)
		}
	}
	if entry.Type == TypeDir && (entry.Position < HeaderSize || entry.Position > uint64(r.fileSize-int64(BlockSize))) {
		return fmt.Errorf("directory position %d is outside archive", entry.Position)
	}
	if entry.NextChain != 0 && (entry.NextChain < HeaderSize || entry.NextChain > uint64(r.fileSize-int64(BlockSize))) {
		return fmt.Errorf("chain position %d is outside archive", entry.NextChain)
	}
	return nil
}

func (r *Pk2Reader) decryptBufferSafe(buffer []byte) error {
	if r.cipher == nil || len(buffer)%blowfish.BlockSize != 0 {
		return fmt.Errorf("%w: encrypted block length is invalid", ErrArchiveHeader)
	}
	for index := 0; index < len(buffer)/blowfish.BlockSize; index++ {
		block := buffer[index*blowfish.BlockSize : (index+1)*blowfish.BlockSize]
		r.cipher.DecryptRev(block, block)
	}
	return nil
}

func safeEntryTime(value uint64) time.Time {
	if value > maxUint63 {
		return time.Time{}
	}
	return time.Unix(int64(value), 0)
}

func (r *Pk2Reader) ReadEntryChecked(entry *PackFileEntry) ([]byte, error) {
	if r == nil || r.file == nil {
		return nil, ErrArchiveClosed
	}
	state := r.readerState()
	state.RLock()
	defer state.RUnlock()
	if state.closed {
		return nil, ErrArchiveClosed
	}
	return r.readEntryLocked(entry)
}

func (r *Pk2Reader) readEntryLocked(entry *PackFileEntry) ([]byte, error) {
	if entry == nil {
		return nil, fmt.Errorf("%w: entry is nil", ErrArchiveEntry)
	}
	if r.fileSize < 0 || entry.Position > maxUint63 || uint64(entry.Size) > safeMaxEntrySize {
		return nil, ErrArchiveBounds
	}
	if entry.Position > uint64(r.fileSize) || uint64(entry.Size) > uint64(r.fileSize)-entry.Position {
		return nil, fmt.Errorf("%w: entry position %d size %d archive size %d", ErrArchiveBounds, entry.Position, entry.Size, r.fileSize)
	}
	buffer := make([]byte, int(entry.Size))
	count, err := r.file.ReadAt(buffer, int64(entry.Position))
	if err != nil || count != len(buffer) {
		if err == nil {
			err = io.ErrUnexpectedEOF
		}
		return nil, fmt.Errorf("%w: read entry: %v", ErrArchiveBounds, err)
	}
	return buffer, nil
}

func (r *Pk2Reader) readerState() *pk2ReaderState {
	if r.state == nil {
		r.state = &pk2ReaderState{}
	}
	return r.state
}

func (r *Pk2Reader) readerCacheMutex() *sync.RWMutex {
	if r.cacheMutex == nil {
		r.cacheMutex = &sync.RWMutex{}
	}
	return r.cacheMutex
}
