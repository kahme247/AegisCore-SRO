package vsro188

import (
	"bytes"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestJobSafeTradeCodecMatchesClientConditionalReadOrder(t *testing.T) {
	progress, err := (JobSafeTradeCodec{}).Encode(SafeTradeUpdate{
		Active:  true,
		Code:    JobSafeTradeCodeProgress,
		Current: 2,
		Limit:   5,
	})
	if err != nil {
		t.Fatal(err)
	}
	if progress.MessageID != opcode.JobUpdateSafeTrade {
		t.Fatalf("progress opcode = 0x%04X", progress.MessageID)
	}
	if got, want := progress.Payload(), []byte{1, 0x50, 2, 5}; !bytes.Equal(got, want) {
		t.Fatalf("progress payload = %X, want %X", got, want)
	}

	limit, err := (JobSafeTradeCodec{}).Encode(SafeTradeUpdate{
		Code: JobSafeTradeErrorLimit,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := limit.Payload(), []byte{0, 0x41}; !bytes.Equal(got, want) {
		t.Fatalf("limit payload = %X, want %X", got, want)
	}

	reset, err := (JobSafeTradeCodec{}).Encode(SafeTradeUpdate{Code: JobSafeTradeCodeTransition40})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := reset.Payload(), []byte{0, 0x40}; !bytes.Equal(got, want) {
		t.Fatalf("reset payload = %X, want %X", got, want)
	}

	restore, err := (JobSafeTradeCodec{}).Encode(SafeTradeUpdate{Active: true, Code: JobSafeTradeCodeRestore51, Current: 3, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := restore.Payload(), []byte{1, 0x51, 3, 5}; !bytes.Equal(got, want) {
		t.Fatalf("restore payload = %X, want %X", got, want)
	}
}

func TestJobSafeTradeCodecRejectsImpossibleState(t *testing.T) {
	for _, update := range []SafeTradeUpdate{
		{Active: true, Code: JobSafeTradeCodeTransition40},
		{Code: JobSafeTradeCodeProgress},
		{Code: 0x42},
	} {
		if _, err := (JobSafeTradeCodec{}).Encode(update); err == nil {
			t.Fatalf("Encode(%#v) accepted an impossible transition", update)
		}
	}
}
