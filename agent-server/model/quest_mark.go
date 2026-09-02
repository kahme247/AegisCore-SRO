package model

import (
	"errors"
	"fmt"
	"sort"
)

var ErrQuestMarkIDExhausted = errors.New("quest mark runtime IDs are exhausted")

// QuestMark is session-owned runtime state. RuntimeID is an opaque uint32
// wire handle; unlike the 32-bit legacy server, AegisCore never exposes a Go
// pointer as the client-visible identity.
type QuestMark struct {
	RuntimeID     uint32
	QuestID       int
	Flags         byte
	Kind          byte
	AuthoredValue uint16
	X             int32
	Y             int32
	Z             int32
	TargetID      uint32
}

func (p *Player) AddQuestMark(mark QuestMark) (QuestMark, error) {
	if p == nil || mark.QuestID <= 0 {
		return QuestMark{}, fmt.Errorf("invalid quest mark owner")
	}
	if mark.RuntimeID != 0 {
		return QuestMark{}, fmt.Errorf("quest mark runtime ID is server-assigned")
	}
	if mark.Flags&0x02 == 0 && mark.TargetID != 0 {
		return QuestMark{}, fmt.Errorf("quest mark target requires flag 0x02")
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.questMarks == nil {
		p.questMarks = make(map[uint32]QuestMark)
	}
	start := p.nextQuestMarkID
	for {
		p.nextQuestMarkID++
		if p.nextQuestMarkID == 0 {
			p.nextQuestMarkID++
		}
		if _, exists := p.questMarks[p.nextQuestMarkID]; !exists {
			mark.RuntimeID = p.nextQuestMarkID
			p.questMarks[mark.RuntimeID] = mark
			return mark, nil
		}
		if p.nextQuestMarkID == start {
			return QuestMark{}, ErrQuestMarkIDExhausted
		}
	}
}

func (p *Player) RemoveQuestMark(runtimeID uint32) (QuestMark, bool) {
	if p == nil || runtimeID == 0 {
		return QuestMark{}, false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	mark, exists := p.questMarks[runtimeID]
	if exists {
		delete(p.questMarks, runtimeID)
	}
	return mark, exists
}

func (p *Player) QuestMarkByRuntimeID(runtimeID uint32) (QuestMark, bool) {
	if p == nil || runtimeID == 0 {
		return QuestMark{}, false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	mark, exists := p.questMarks[runtimeID]
	return mark, exists
}

// RemoveQuestMarksByQuest mirrors GameServer 0x00579950: every mark owned by
// the quest is removed and returned in stable runtime-handle order so callers
// can emit one 0x30D7 packet per record.
func (p *Player) RemoveQuestMarksByQuest(questID int) []QuestMark {
	if p == nil || questID <= 0 {
		return nil
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	removed := make([]QuestMark, 0)
	for runtimeID, mark := range p.questMarks {
		if mark.QuestID == questID {
			removed = append(removed, mark)
			delete(p.questMarks, runtimeID)
		}
	}
	sort.Slice(removed, func(i, j int) bool { return removed[i].RuntimeID < removed[j].RuntimeID })
	return removed
}

func (p *Player) QuestMarksSnapshot() []QuestMark {
	if p == nil {
		return nil
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	marks := make([]QuestMark, 0, len(p.questMarks))
	for _, mark := range p.questMarks {
		marks = append(marks, mark)
	}
	sort.Slice(marks, func(i, j int) bool { return marks[i].RuntimeID < marks[j].RuntimeID })
	return marks
}
