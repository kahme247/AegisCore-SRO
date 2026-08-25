// Package questscript reads the supplied Lua 5.1 quest chunks without
// executing them. It is deliberately a decoder, not a general Lua runtime.
package questscript

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

const (
	maxChunkBytes  = 32 << 20
	maxCollection  = 1 << 20
	maxStringBytes = 1 << 20
)

var ErrInvalidChunk = errors.New("invalid Lua 5.1 quest chunk")

type Value struct {
	Kind   ValueKind
	String string
	Number float64
	Bool   bool
}

type ValueKind byte

const (
	ValueUnknown ValueKind = iota
	ValueNil
	ValueBool
	ValueNumber
	ValueString
	ValueGlobal
)

type Call struct {
	Function string
	Args     []Value
}

type Prototype struct {
	Source     string
	MaxStack   byte
	Code       []uint32
	Constants  []Value
	Prototypes []Prototype
}

type Chunk struct{ Main Prototype }

func Decode(reader io.Reader) (Chunk, error) {
	if reader == nil {
		return Chunk{}, fmt.Errorf("%w: reader is nil", ErrInvalidChunk)
	}
	contents, err := io.ReadAll(io.LimitReader(reader, maxChunkBytes+1))
	if err != nil {
		return Chunk{}, err
	}
	if len(contents) > maxChunkBytes {
		return Chunk{}, fmt.Errorf("%w: chunk exceeds %d bytes", ErrInvalidChunk, maxChunkBytes)
	}
	decoder := chunkDecoder{data: contents}
	if err := decoder.header(); err != nil {
		return Chunk{}, err
	}
	main, err := decoder.prototype("")
	if err != nil {
		return Chunk{}, err
	}
	if decoder.offset != len(decoder.data) {
		return Chunk{}, fmt.Errorf("%w: %d trailing bytes", ErrInvalidChunk, len(decoder.data)-decoder.offset)
	}
	return Chunk{Main: main}, nil
}

type chunkDecoder struct {
	data   []byte
	offset int
}

func (d *chunkDecoder) header() error {
	header, err := d.take(12)
	if err != nil {
		return err
	}
	want := []byte{0x1b, 'L', 'u', 'a', 0x51, 0, 1, 4, 4, 4, 8, 0}
	for i := range want {
		if header[i] != want[i] {
			return fmt.Errorf("%w: unsupported header byte %d", ErrInvalidChunk, i)
		}
	}
	return nil
}

func (d *chunkDecoder) prototype(parentSource string) (Prototype, error) {
	source, err := d.luaString()
	if err != nil {
		return Prototype{}, err
	}
	if source == "" {
		source = parentSource
	}
	if _, err = d.take(8); err != nil { // line-defined, last-line-defined
		return Prototype{}, err
	}
	metadata, err := d.take(4)
	if err != nil {
		return Prototype{}, err
	}
	prototype := Prototype{Source: source, MaxStack: metadata[3]}
	if prototype.MaxStack == 0 {
		return Prototype{}, fmt.Errorf("%w: prototype has zero stack", ErrInvalidChunk)
	}
	codeCount, err := d.count()
	if err != nil {
		return Prototype{}, err
	}
	prototype.Code = make([]uint32, codeCount)
	for i := range prototype.Code {
		prototype.Code[i], err = d.uint32()
		if err != nil {
			return Prototype{}, err
		}
	}
	constantCount, err := d.count()
	if err != nil {
		return Prototype{}, err
	}
	prototype.Constants = make([]Value, constantCount)
	for i := range prototype.Constants {
		kind, takeErr := d.take(1)
		if takeErr != nil {
			return Prototype{}, takeErr
		}
		switch kind[0] {
		case 0:
			prototype.Constants[i] = Value{Kind: ValueNil}
		case 1:
			value, takeErr := d.take(1)
			if takeErr != nil {
				return Prototype{}, takeErr
			}
			prototype.Constants[i] = Value{Kind: ValueBool, Bool: value[0] != 0}
		case 3:
			bits, takeErr := d.uint64()
			if takeErr != nil {
				return Prototype{}, takeErr
			}
			prototype.Constants[i] = Value{Kind: ValueNumber, Number: math.Float64frombits(bits)}
		case 4:
			value, takeErr := d.luaString()
			if takeErr != nil {
				return Prototype{}, takeErr
			}
			prototype.Constants[i] = Value{Kind: ValueString, String: value}
		default:
			return Prototype{}, fmt.Errorf("%w: constant %d has type %d", ErrInvalidChunk, i, kind[0])
		}
	}
	childCount, err := d.count()
	if err != nil {
		return Prototype{}, err
	}
	prototype.Prototypes = make([]Prototype, childCount)
	for i := range prototype.Prototypes {
		prototype.Prototypes[i], err = d.prototype(source)
		if err != nil {
			return Prototype{}, err
		}
	}
	lineCount, err := d.count()
	if err != nil {
		return Prototype{}, err
	}
	if _, err = d.take(lineCount * 4); err != nil {
		return Prototype{}, err
	}
	localCount, err := d.count()
	if err != nil {
		return Prototype{}, err
	}
	for i := 0; i < localCount; i++ {
		if _, err = d.luaString(); err != nil {
			return Prototype{}, err
		}
		if _, err = d.take(8); err != nil {
			return Prototype{}, err
		}
	}
	upvalueCount, err := d.count()
	if err != nil {
		return Prototype{}, err
	}
	for i := 0; i < upvalueCount; i++ {
		if _, err = d.luaString(); err != nil {
			return Prototype{}, err
		}
	}
	return prototype, nil
}

func (d *chunkDecoder) luaString() (string, error) {
	length, err := d.uint32()
	if err != nil {
		return "", err
	}
	if length == 0 {
		return "", nil
	}
	if length > maxStringBytes {
		return "", fmt.Errorf("%w: string length %d exceeds limit", ErrInvalidChunk, length)
	}
	value, err := d.take(int(length))
	if err != nil {
		return "", err
	}
	if value[len(value)-1] != 0 {
		return "", fmt.Errorf("%w: string is not terminated", ErrInvalidChunk)
	}
	return string(value[:len(value)-1]), nil
}

func (d *chunkDecoder) count() (int, error) {
	value, err := d.uint32()
	if err != nil {
		return 0, err
	}
	if value > maxCollection {
		return 0, fmt.Errorf("%w: collection count %d exceeds limit", ErrInvalidChunk, value)
	}
	return int(value), nil
}

func (d *chunkDecoder) uint32() (uint32, error) {
	value, err := d.take(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(value), nil
}

func (d *chunkDecoder) uint64() (uint64, error) {
	value, err := d.take(8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(value), nil
}

func (d *chunkDecoder) take(length int) ([]byte, error) {
	if length < 0 || d.offset > len(d.data)-length {
		return nil, fmt.Errorf("%w: truncated at byte %d", ErrInvalidChunk, d.offset)
	}
	value := d.data[d.offset : d.offset+length]
	d.offset += length
	return value, nil
}
