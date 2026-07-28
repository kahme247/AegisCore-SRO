package server

import (
	"errors"
	"testing"
	"time"
)

func TestSessionTrafficFilterRejectsPacketBeyondWindowLimit(t *testing.T) {
	now := time.Unix(100, 0)
	filter := newSessionTrafficFilter(TrafficFilterOptions{Window: time.Second, PacketLimit: 2})
	filter.now = func() time.Time { return now }

	if err := filter.Admit(); err != nil {
		t.Fatalf("first packet rejected: %v", err)
	}
	if err := filter.Admit(); err != nil {
		t.Fatalf("second packet rejected: %v", err)
	}
	err := filter.Admit()
	if err == nil {
		t.Fatal("third packet accepted beyond limit")
	}
	var limitErr *TrafficLimitError
	if !errors.As(err, &limitErr) {
		t.Fatalf("error type = %T, want *TrafficLimitError", err)
	}
	if limitErr.Observed != 3 || limitErr.Limit != 2 || limitErr.Window != time.Second {
		t.Fatalf("limit error = %+v", limitErr)
	}
}

func TestSessionTrafficFilterResetsAfterWindow(t *testing.T) {
	now := time.Unix(100, 0)
	filter := newSessionTrafficFilter(TrafficFilterOptions{Window: time.Second, PacketLimit: 1})
	filter.now = func() time.Time { return now }

	if err := filter.Admit(); err != nil {
		t.Fatalf("first packet rejected: %v", err)
	}
	now = now.Add(time.Second)
	if err := filter.Admit(); err != nil {
		t.Fatalf("packet in next window rejected: %v", err)
	}
}

func TestTrafficFilterDefaultsInvalidOptions(t *testing.T) {
	filter := newSessionTrafficFilter(TrafficFilterOptions{})
	if filter.window != DefaultTrafficFilterWindow {
		t.Fatalf("window = %s, want %s", filter.window, DefaultTrafficFilterWindow)
	}
	if filter.packetLimit != DefaultTrafficFilterPacketLimit {
		t.Fatalf("packet limit = %d, want %d", filter.packetLimit, DefaultTrafficFilterPacketLimit)
	}
}
