package service

import (
	"context"
	"testing"
	"time"
)

type benchmarkPositionRepository struct {
	delay time.Duration
}

func (r benchmarkPositionRepository) UpdateCharacterPosition(int, int16, float32, float32, float32) error {
	if r.delay > 0 {
		time.Sleep(r.delay)
	}
	return nil
}

// BenchmarkPositionPersistenceFlushDue measures a complete checkpoint cycle:
// coalescing one dirty position per player, dispatching the asynchronous flush,
// and waiting for the claimed batch to settle.
// Including queue population keeps benchmark calibration representative while
// exposing end-to-end checkpoint throughput.
func BenchmarkPositionPersistenceFlushDue(b *testing.B) {
	benchmarks := []struct {
		name    string
		players int
		delay   time.Duration
	}{
		{name: "NoRepositoryLatency/Players=1", players: 1},
		{name: "NoRepositoryLatency/Players=100", players: 100},
		{name: "NoRepositoryLatency/Players=1000", players: 1000},
		{name: "NoRepositoryLatency/Players=4000", players: 4000},
		{name: "RepositoryLatency=1ms/Players=1", players: 1, delay: time.Millisecond},
		{name: "RepositoryLatency=1ms/Players=10", players: 10, delay: time.Millisecond},
		{name: "RepositoryLatency=1ms/Players=4000", players: 4000, delay: time.Millisecond},
	}

	for _, benchmark := range benchmarks {
		b.Run(benchmark.name, func(b *testing.B) {
			service := NewPositionPersistenceService(
				benchmarkPositionRepository{delay: benchmark.delay},
				time.Nanosecond,
			)
			base := time.Now().Add(time.Second)

			b.ReportAllocs()
			b.ReportMetric(float64(benchmark.players), "writes/op")
			for iteration := 0; iteration < b.N; iteration++ {
				for player := 0; player < benchmark.players; player++ {
					uid := uint32(player + 1)
					service.MarkDirty(persistenceTestPlayer(
						uid,
						player+1,
						float32(iteration+player),
					))
				}
				service.FlushDue(base.Add(time.Duration(iteration+1) * time.Nanosecond))
				if err := service.WaitForIdle(context.Background()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
