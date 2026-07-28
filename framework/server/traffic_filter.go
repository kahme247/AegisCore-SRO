package server

import (
	"fmt"
	"sync"
	"time"
)

const (
	DefaultTrafficFilterWindow      = time.Second
	DefaultTrafficFilterPacketLimit = uint32(200)
)

// TrafficFilterOptions defines the connection-local packet-count window.
// It intentionally counts physical protocol frames, so massive wrappers and
// core handshake packets cannot bypass the budget.
type TrafficFilterOptions struct {
	Window      time.Duration
	PacketLimit uint32
}

func DefaultTrafficFilterOptions() TrafficFilterOptions {
	return TrafficFilterOptions{
		Window:      DefaultTrafficFilterWindow,
		PacketLimit: DefaultTrafficFilterPacketLimit,
	}
}

func normalizeTrafficFilterOptions(options TrafficFilterOptions) TrafficFilterOptions {
	if options.Window <= 0 {
		options.Window = DefaultTrafficFilterWindow
	}
	if options.PacketLimit == 0 {
		options.PacketLimit = DefaultTrafficFilterPacketLimit
	}
	return options
}

type sessionTrafficFilter struct {
	mu          sync.Mutex
	window      time.Duration
	packetLimit uint32
	windowStart time.Time
	packetCount uint32
	now         func() time.Time
}

// TrafficLimitError is returned for the first physical frame beyond a
// connection's configured budget. Its fields are suitable for structured
// logging and metrics without parsing an error string.
type TrafficLimitError struct {
	Observed uint32
	Limit    uint32
	Window   time.Duration
}

func (err *TrafficLimitError) Error() string {
	return fmt.Sprintf("traffic filter observed %d packets, limit %d in %s", err.Observed, err.Limit, err.Window)
}

func newSessionTrafficFilter(options TrafficFilterOptions) *sessionTrafficFilter {
	options = normalizeTrafficFilterOptions(options)
	return &sessionTrafficFilter{
		window:      options.Window,
		packetLimit: options.PacketLimit,
		now:         time.Now,
	}
}

func (filter *sessionTrafficFilter) Admit() error {
	if filter == nil {
		return nil
	}
	filter.mu.Lock()
	defer filter.mu.Unlock()

	now := filter.now()
	if filter.windowStart.IsZero() || now.Sub(filter.windowStart) >= filter.window || now.Before(filter.windowStart) {
		filter.windowStart = now
		filter.packetCount = 0
	}
	filter.packetCount++
	if filter.packetCount > filter.packetLimit {
		return &TrafficLimitError{
			Observed: filter.packetCount,
			Limit:    filter.packetLimit,
			Window:   filter.window,
		}
	}
	return nil
}
