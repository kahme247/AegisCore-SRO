package model

import (
	"fmt"
	"math"
)

const (
	QuestTimerInactive uint32 = math.MaxUint32
	QuestTimerModeOne  byte   = 1
	QuestTimerModeTwo  byte   = 2
	QuestTimerModeFour byte   = 4
)

// PackQuestTimerDuration reproduces GameServer 0x00570650. Authored durations
// are seconds: the start path sends duration/60 and duration%60 as minutes and
// seconds, while this packer further splits the minute quotient at 24 hours.
// The packed remainders occupy bits 20, 15, and 10.
func PackQuestTimerDuration(duration uint32) uint32 {
	quotient60 := duration / 60
	remainder60 := duration % 60
	quotient24 := quotient60 / 24
	remainder24 := quotient60 % 24
	return (remainder60&0x3f)<<20 | (remainder24&0x1f)<<15 | (quotient24&0x1f)<<10
}

type QuestTimer struct {
	QuestID     int
	Mode        byte
	Remaining   uint32
	PackedValue uint32
	Event       QuestAuthoredEvent
	Generation  uint64
}

type QuestTimerTick struct {
	Timer         QuestTimer
	DispatchEvent bool
	DisplayUpdate bool
}

// StartQuestTimer mirrors the common setup selection: positive durations use
// event 0x0E; zero-duration modes 1/4 also use 0x0E; zero-duration mode 2 uses
// authored event 0x18. Other zero-duration modes create no timer.
func (p *Player) StartQuestTimer(questID int, duration uint32, mode byte) (QuestTimer, bool, error) {
	if p == nil || questID <= 0 || duration > math.MaxUint8 {
		return QuestTimer{}, false, fmt.Errorf("invalid quest timer")
	}
	event := QuestAuthoredEvent(0)
	if duration > 0 || mode == QuestTimerModeOne || mode == QuestTimerModeFour {
		event = QuestAuthoredCountdown
	} else if mode == QuestTimerModeTwo {
		event = QuestAuthoredMode2Timer
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	p.questTimerGeneration++
	if p.questTimerGeneration == 0 {
		p.questTimerGeneration++
	}
	if event == 0 {
		delete(p.questTimers, questID)
		return QuestTimer{}, false, nil
	}
	if p.questTimers == nil {
		p.questTimers = make(map[int]QuestTimer)
	}
	timer := QuestTimer{
		QuestID: questID, Mode: mode, Remaining: duration,
		PackedValue: PackQuestTimerDuration(duration), Event: event,
		Generation: p.questTimerGeneration,
	}
	p.questTimers[questID] = timer
	return timer, true, nil
}

// TickQuestTimer is generation-safe: a callback retained from an older timer
// cannot decrement or expire its replacement. Event 0x18 is a one-shot
// authored callback; event 0x0E performs the common countdown behavior.
func (p *Player) TickQuestTimer(questID int, generation uint64) (QuestTimerTick, bool) {
	if p == nil || questID <= 0 || generation == 0 {
		return QuestTimerTick{}, false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	timer, exists := p.questTimers[questID]
	if !exists || timer.Generation != generation {
		return QuestTimerTick{}, false
	}
	result := QuestTimerTick{Timer: timer}
	if timer.Event == QuestAuthoredMode2Timer {
		delete(p.questTimers, questID)
		result.DispatchEvent = true
		return result, true
	}
	if timer.Remaining > 0 {
		timer.Remaining--
	}
	if timer.Remaining == 0 {
		delete(p.questTimers, questID)
		timer.PackedValue = 0
		result.Timer = timer
		result.DispatchEvent = true
		return result, true
	}
	timer.PackedValue = PackQuestTimerDuration(timer.Remaining)
	p.questTimers[questID] = timer
	result.Timer = timer
	result.DisplayUpdate = timer.Remaining%10 == 0
	return result, true
}

func (p *Player) CancelQuestTimer(questID int) (QuestTimer, bool) {
	if p == nil || questID <= 0 {
		return QuestTimer{}, false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	timer, exists := p.questTimers[questID]
	if exists {
		delete(p.questTimers, questID)
		p.questTimerGeneration++
		if p.questTimerGeneration == 0 {
			p.questTimerGeneration++
		}
	}
	return timer, exists
}

func (p *Player) QuestTimerSnapshot(questID int) (QuestTimer, bool) {
	if p == nil || questID <= 0 {
		return QuestTimer{}, false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	timer, exists := p.questTimers[questID]
	return timer, exists
}
