package questscript

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"testing"
)

func TestDecodeAndRecoverDeclarativeCall(t *testing.T) {
	constants := []Value{
		{Kind: ValueString, String: "LuaGetQuestID"},
		{Kind: ValueString, String: "QNO_TEST"},
		{Kind: ValueNumber, Number: 140},
	}
	code := []uint32{
		instructionABx(5, 0, 0), // GETGLOBAL R0, LuaGetQuestID
		instructionABx(1, 1, 1), // LOADK R1, QNO_TEST
		instructionABC(28, 0, 2, 1),
	}
	contents := testChunk(t, Prototype{Source: "@QNO_TEST.lua", MaxStack: 2, Code: code, Constants: constants})
	chunk, err := Decode(bytes.NewReader(contents))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	calls := Calls(chunk.Main)
	if len(calls) != 1 || calls[0].Function != "LuaGetQuestID" || len(calls[0].Args) != 1 || calls[0].Args[0].String != "QNO_TEST" {
		t.Fatalf("calls = %#v", calls)
	}
}

func TestDecodeRejectsUnsupportedHeaderAndTrailingBytes(t *testing.T) {
	valid := testChunk(t, Prototype{Source: "@empty.lua", MaxStack: 1})
	badHeader := append([]byte(nil), valid...)
	badHeader[4] = 0x50
	if _, err := Decode(bytes.NewReader(badHeader)); !errors.Is(err, ErrInvalidChunk) {
		t.Fatalf("bad header error = %v", err)
	}
	withTrailing := append(append([]byte(nil), valid...), 1)
	if _, err := Decode(bytes.NewReader(withTrailing)); !errors.Is(err, ErrInvalidChunk) {
		t.Fatalf("trailing-byte error = %v", err)
	}
}

func testChunk(t *testing.T, prototype Prototype) []byte {
	t.Helper()
	buffer := bytes.NewBuffer([]byte{0x1b, 'L', 'u', 'a', 0x51, 0, 1, 4, 4, 4, 8, 0})
	writeTestPrototype(t, buffer, prototype)
	return buffer.Bytes()
}

func writeTestPrototype(t *testing.T, buffer *bytes.Buffer, prototype Prototype) {
	t.Helper()
	writeTestString(t, buffer, prototype.Source)
	writeTestUint32(t, buffer, 0)
	writeTestUint32(t, buffer, 0)
	buffer.Write([]byte{0, 0, 0, prototype.MaxStack})
	writeTestUint32(t, buffer, uint32(len(prototype.Code)))
	for _, instruction := range prototype.Code {
		writeTestUint32(t, buffer, instruction)
	}
	writeTestUint32(t, buffer, uint32(len(prototype.Constants)))
	for _, constant := range prototype.Constants {
		switch constant.Kind {
		case ValueNil:
			buffer.WriteByte(0)
		case ValueBool:
			buffer.WriteByte(1)
			if constant.Bool {
				buffer.WriteByte(1)
			} else {
				buffer.WriteByte(0)
			}
		case ValueNumber:
			buffer.WriteByte(3)
			var encoded [8]byte
			binary.LittleEndian.PutUint64(encoded[:], math.Float64bits(constant.Number))
			buffer.Write(encoded[:])
		case ValueString:
			buffer.WriteByte(4)
			writeTestString(t, buffer, constant.String)
		default:
			t.Fatalf("unsupported test constant %#v", constant)
		}
	}
	writeTestUint32(t, buffer, uint32(len(prototype.Prototypes)))
	for _, child := range prototype.Prototypes {
		writeTestPrototype(t, buffer, child)
	}
	writeTestUint32(t, buffer, 0) // line info
	writeTestUint32(t, buffer, 0) // locals
	writeTestUint32(t, buffer, 0) // upvalues
}

func writeTestString(t *testing.T, buffer *bytes.Buffer, value string) {
	t.Helper()
	writeTestUint32(t, buffer, uint32(len(value)+1))
	buffer.WriteString(value)
	buffer.WriteByte(0)
}

func writeTestUint32(t *testing.T, buffer *bytes.Buffer, value uint32) {
	t.Helper()
	var encoded [4]byte
	binary.LittleEndian.PutUint32(encoded[:], value)
	buffer.Write(encoded[:])
}

func instructionABC(opcode, a, b, c uint32) uint32 {
	return opcode | a<<6 | c<<14 | b<<23
}

func instructionABx(opcode, a, bx uint32) uint32 {
	return opcode | a<<6 | bx<<14
}
