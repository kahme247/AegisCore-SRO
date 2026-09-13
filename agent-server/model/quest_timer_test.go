package model

import "testing"

func TestPackQuestTimerDuration(t *testing.T) {
	tests := []struct {
		duration uint32
		want     uint32
	}{
		{duration: 0, want: 0},
		{duration: 59, want: 59 << 20},
		{duration: 60, want: 1 << 15},
		{duration: 1440, want: 1 << 10},
		{duration: 1505, want: 5<<20 | 1<<15 | 1<<10},
	}
	for _, test := range tests {
		if got := PackQuestTimerDuration(test.duration); got != test.want {
			t.Errorf("PackQuestTimerDuration(%d) = 0x%08X, want 0x%08X", test.duration, got, test.want)
		}
	}
}

func TestStartQuestTimerSelectsBinaryAuthoredEvent(t *testing.T) {
	tests := []struct {
		name      string
		duration  uint32
		mode      byte
		wantEvent QuestAuthoredEvent
		wantTimer bool
	}{
		{name: "positive duration", duration: 30, mode: 0xff, wantEvent: QuestAuthoredCountdown, wantTimer: true},
		{name: "zero mode one", mode: QuestTimerModeOne, wantEvent: QuestAuthoredCountdown, wantTimer: true},
		{name: "zero mode four", mode: QuestTimerModeFour, wantEvent: QuestAuthoredCountdown, wantTimer: true},
		{name: "zero mode two", mode: QuestTimerModeTwo, wantEvent: QuestAuthoredMode2Timer, wantTimer: true},
		{name: "unsupported zero mode", mode: 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			player := &Player{}
			timer, created, err := player.StartQuestTimer(100, test.duration, test.mode)
			if err != nil {
				t.Fatalf("StartQuestTimer() error = %v", err)
			}
			if created != test.wantTimer {
				t.Fatalf("StartQuestTimer() created = %v, want %v", created, test.wantTimer)
			}
			if created && (timer.Event != test.wantEvent || timer.Remaining != test.duration || timer.Generation == 0) {
				t.Fatalf("StartQuestTimer() = %+v", timer)
			}
		})
	}

	if _, _, err := (&Player{}).StartQuestTimer(100, 256, QuestTimerModeOne); err == nil {
		t.Fatal("StartQuestTimer accepted duration outside authored uint8 range")
	}
}

func TestQuestTimerCountdownAndGenerationSafety(t *testing.T) {
	player := &Player{}
	first, _, err := player.StartQuestTimer(100, 11, QuestTimerModeOne)
	if err != nil {
		t.Fatal(err)
	}
	tick, ok := player.TickQuestTimer(100, first.Generation)
	if !ok || tick.Timer.Remaining != 10 || !tick.DisplayUpdate || tick.DispatchEvent {
		t.Fatalf("first tick = (%+v, %v)", tick, ok)
	}
	tick, ok = player.TickQuestTimer(100, first.Generation)
	if !ok || tick.Timer.Remaining != 9 || tick.DisplayUpdate || tick.DispatchEvent {
		t.Fatalf("second tick = (%+v, %v)", tick, ok)
	}

	replacement, _, err := player.StartQuestTimer(100, 1, QuestTimerModeFour)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := player.TickQuestTimer(100, first.Generation); ok {
		t.Fatal("stale generation ticked replacement timer")
	}
	tick, ok = player.TickQuestTimer(100, replacement.Generation)
	if !ok || !tick.DispatchEvent || tick.DisplayUpdate || tick.Timer.Remaining != 0 {
		t.Fatalf("expiry tick = (%+v, %v)", tick, ok)
	}
	if _, exists := player.QuestTimerSnapshot(100); exists {
		t.Fatal("expired timer remains registered")
	}
}

func TestQuestTimerModeTwoAndCancellation(t *testing.T) {
	player := &Player{}
	timer, _, err := player.StartQuestTimer(101, 0, QuestTimerModeTwo)
	if err != nil {
		t.Fatal(err)
	}
	tick, ok := player.TickQuestTimer(101, timer.Generation)
	if !ok || !tick.DispatchEvent || tick.DisplayUpdate || tick.Timer.Event != QuestAuthoredMode2Timer {
		t.Fatalf("mode-two tick = (%+v, %v)", tick, ok)
	}
	if _, ok := player.TickQuestTimer(101, timer.Generation); ok {
		t.Fatal("mode-two timer dispatched more than once")
	}

	timer, _, err = player.StartQuestTimer(102, 20, QuestTimerModeOne)
	if err != nil {
		t.Fatal(err)
	}
	removed, ok := player.CancelQuestTimer(102)
	if !ok || removed.Generation != timer.Generation {
		t.Fatalf("CancelQuestTimer() = (%+v, %v)", removed, ok)
	}
	if _, ok := player.TickQuestTimer(102, timer.Generation); ok {
		t.Fatal("cancelled timer accepted stale callback")
	}
}
