package network

import "io"

const packetReaderBufferSize = DefaultMaxFramePayload + int(HeaderSize) + 8

// PacketReader adapts an io.Reader TCP stream into complete packets. It owns a
// persistent FrameDecoder because a TCP read is not a packet boundary: headers
// and payloads can be fragmented or several frames can be coalesced in one
// read.
//
// ReadPackets returns successfully decoded packets before a terminal reader
// error. The error is returned on the following call so existing session loops
// can dispatch the final packets without having to special-case n > 0, EOF.
// A PacketReader must have a single read owner.
type PacketReader struct {
	reader      io.Reader
	decoder     *FrameDecoder
	readBuffer  []byte
	terminalErr error
}

func NewPacketReader(reader io.Reader) *PacketReader {
	return &PacketReader{
		reader:     reader,
		decoder:    NewFrameDecoder(),
		readBuffer: make([]byte, packetReaderBufferSize),
	}
}

func (p *PacketReader) ReadPackets() ([]Packet, error) {
	if p.terminalErr != nil {
		return nil, p.terminalErr
	}

	for {
		bytesRead, readErr := p.reader.Read(p.readBuffer)
		if bytesRead < 0 || bytesRead > len(p.readBuffer) {
			return nil, io.ErrShortBuffer
		}

		packets := make([]Packet, 0)
		if bytesRead > 0 {
			frames, decodeErr := p.decoder.Push(p.readBuffer[:bytesRead])
			packets = packetsFromFrames(frames)
			if decodeErr != nil {
				return p.finish(packets, decodeErr)
			}
		}

		if readErr != nil {
			terminalErr := readErr
			if finishErr := p.decoder.Finish(); finishErr != nil {
				terminalErr = finishErr
			}
			return p.finish(packets, terminalErr)
		}

		if len(packets) > 0 {
			return packets, nil
		}

		// io.Reader implementations should not return (0, nil). Treat it as a
		// terminal no-progress condition rather than making the caller spin.
		if bytesRead == 0 {
			return nil, io.ErrNoProgress
		}
	}
}

func (p *PacketReader) finish(packets []Packet, err error) ([]Packet, error) {
	p.terminalErr = err
	if len(packets) > 0 {
		return packets, nil
	}
	return nil, err
}

func packetsFromFrames(frames [][]byte) []Packet {
	packets := make([]Packet, 0, len(frames))
	for _, frame := range frames {
		packets = append(packets, NewPacket(frame))
	}
	return packets
}
