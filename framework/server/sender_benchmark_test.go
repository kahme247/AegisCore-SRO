package server

import (
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/framework/network"
)

const senderFanoutBenchmarkSessions = 10_000

type benchmarkDeadlineWriter struct {
	delay time.Duration
}

func (w benchmarkDeadlineWriter) Write(bytes []byte) (int, error) {
	if w.delay > 0 {
		time.Sleep(w.delay)
	}
	return len(bytes), nil
}

func (benchmarkDeadlineWriter) SetWriteDeadline(time.Time) error { return nil }

func benchmarkPacketEncoder(packet network.Packet) ([]byte, error) {
	return packet.MarshalBinary()
}

func newSenderBenchmarkPacket() network.Packet {
	packet := network.NewServerPacket(0xB021)
	_ = packet.WriteUInt32(12345)
	_ = packet.WriteUInt16(32000)
	_ = packet.WriteFloat32(12.5)
	return *packet
}

func BenchmarkSenderSend(b *testing.B) {
	for _, benchmark := range []struct {
		name  string
		delay time.Duration
	}{
		{name: "FastWriter"},
		{name: "SlowWriter=1ms", delay: time.Millisecond},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			sender := NewSender(benchmarkDeadlineWriter{delay: benchmark.delay}, benchmarkPacketEncoder)
			b.Cleanup(func() {
				sender.Close()
				sender.Wait()
			})
			packet := newSenderBenchmarkPacket()
			b.ReportAllocs()
			for b.Loop() {
				if err := sender.Send(packet); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkSenderFanout10K(b *testing.B) {
	benchmarks := []struct {
		name          string
		slowSession   int
		slowWriteTime time.Duration
	}{
		{name: "FastWriters", slowSession: -1},
		{name: "OneSlowWriter=1ms", slowSession: senderFanoutBenchmarkSessions / 2, slowWriteTime: time.Millisecond},
	}

	for _, benchmark := range benchmarks {
		b.Run(benchmark.name, func(b *testing.B) {
			senders := make([]*Sender, senderFanoutBenchmarkSessions)
			for index := range senders {
				delay := time.Duration(0)
				if index == benchmark.slowSession {
					delay = benchmark.slowWriteTime
				}
				senders[index] = NewSender(benchmarkDeadlineWriter{delay: delay}, benchmarkPacketEncoder)
			}
			b.Cleanup(func() {
				for _, sender := range senders {
					sender.Close()
				}
				for _, sender := range senders {
					sender.Wait()
				}
			})

			packet := newSenderBenchmarkPacket()
			b.ReportAllocs()
			b.ReportMetric(senderFanoutBenchmarkSessions, "sessions/op")
			for b.Loop() {
				for _, sender := range senders {
					if err := sender.Send(packet); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

func BenchmarkSenderTrySendFanout10K(b *testing.B) {
	senders := make([]*Sender, senderFanoutBenchmarkSessions)
	for index := range senders {
		senders[index] = NewSender(benchmarkDeadlineWriter{}, benchmarkPacketEncoder)
	}
	b.Cleanup(func() {
		for _, sender := range senders {
			sender.Close()
		}
		for _, sender := range senders {
			sender.Wait()
		}
	})

	packet := newSenderBenchmarkPacket()
	b.ReportAllocs()
	b.ReportMetric(senderFanoutBenchmarkSessions, "sessions/op")
	for b.Loop() {
		for _, sender := range senders {
			if err := sender.TrySend(packet); err != nil {
				b.Fatal(err)
			}
		}
	}
}
