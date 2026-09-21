package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/Harvi8/AegisCore/download-server/catalog"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	log "github.com/sirupsen/logrus"
)

const DefaultChunkSize = 16 * 1024

type PreparedFileCatalog interface {
	Open(id uint32) (catalog.PreparedFile, uint64, error)
}

type PacketSender interface {
	Send(packet network.Packet) error
}

type FileRequestHandler struct {
	Catalog   PreparedFileCatalog
	ChunkSize int
	Limiter   *TransferLimiter
	Audit     func(TransferAudit)
}

type TransferAudit struct {
	Client    string
	FileID    uint32
	Offset    uint32
	BytesSent uint64
	Success   bool
	Reason    string
	Duration  time.Duration
}

func (h *FileRequestHandler) Handle(ctx context.Context, session *server.Session, packet network.Packet) error {
	client := "unknown"
	if session != nil && session.Conn != nil && session.Conn.RemoteAddr() != nil {
		client = session.Conn.RemoteAddr().String()
		if host, _, err := net.SplitHostPort(client); err == nil {
			client = host
		}
	}
	return h.transfer(ctx, session, packet, client)
}

func (h *FileRequestHandler) Transfer(ctx context.Context, sender PacketSender, packet network.Packet) error {
	return h.transfer(ctx, sender, packet, "unknown")
}

func (h *FileRequestHandler) transfer(ctx context.Context, sender PacketSender, packet network.Packet, client string) (returnErr error) {
	audit := TransferAudit{Client: client}
	started := time.Now()
	defer func() {
		audit.Duration = time.Since(started)
		if returnErr != nil && audit.Reason == "" {
			audit.Reason = returnErr.Error()
		}
		if h.Audit != nil {
			h.Audit(audit)
		}
	}()
	if packet.PayloadLen() != 8 {
		return fmt.Errorf("invalid download request payload length %d", packet.PayloadLen())
	}
	fileID, err := packet.ReadUInt32()
	if err != nil {
		return fmt.Errorf("read patch file ID: %w", err)
	}
	audit.FileID = fileID
	offset, err := packet.ReadUInt32()
	if err != nil {
		return fmt.Errorf("read patch resume offset: %w", err)
	}
	audit.Offset = offset
	if packet.Remaining() != 0 {
		return errors.New("download request contains trailing data")
	}
	if h.Catalog == nil {
		return errors.New("download catalog is not configured")
	}
	release, err := h.Limiter.Acquire(client)
	if err != nil {
		audit.Reason = "concurrency-limit"
		return sendFailureWithCause(sender, err)
	}
	defer release()

	file, size, err := h.Catalog.Open(fileID)
	if err != nil {
		audit.Reason = "catalog"
		return sendFailureWithCause(sender, err)
	}
	defer file.Close()
	if uint64(offset) > size {
		audit.Reason = "offset"
		return sendCompletion(sender, false)
	}
	if _, err := file.Seek(int64(offset), io.SeekStart); err != nil {
		_ = sendCompletion(sender, false)
		return fmt.Errorf("seek patch file %d: %w", fileID, err)
	}

	chunkSize := h.ChunkSize
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	if chunkSize > network.DefaultMaxFramePayload {
		return fmt.Errorf("download chunk size %d exceeds protocol maximum %d", chunkSize, network.DefaultMaxFramePayload)
	}
	buffer := make([]byte, chunkSize)
	remaining := size - uint64(offset)
	for remaining > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		want := uint64(len(buffer))
		if remaining < want {
			want = remaining
		}
		read, readErr := io.ReadFull(file, buffer[:int(want)])
		if readErr != nil {
			_ = sendCompletion(sender, false)
			return fmt.Errorf("read patch file %d: %w", fileID, readErr)
		}
		chunk := network.EmptyPacket()
		chunk.MessageID = opcode.DownloadFileChunk
		if err := chunk.WriteBytes(buffer[:read]); err != nil {
			return err
		}
		if err := sender.Send(chunk); err != nil {
			return err
		}
		audit.BytesSent += uint64(read)
		remaining -= uint64(read)
	}
	if err := sendCompletion(sender, true); err != nil {
		return err
	}
	audit.Success = true
	audit.Reason = "success"
	return nil
}

func LogTransferAudit(audit TransferAudit) {
	log.WithFields(log.Fields{
		"client": audit.Client, "file_id": audit.FileID, "offset": audit.Offset,
		"bytes": audit.BytesSent, "success": audit.Success, "reason": audit.Reason,
		"duration_ms": audit.Duration.Milliseconds(),
	}).Info("patch transfer completed")
}

// Keep the concrete catalog contract checked at compile time while retaining
// a small interface for deterministic handler tests.
var _ PreparedFileCatalog = (*catalog.FileCatalog)(nil)
