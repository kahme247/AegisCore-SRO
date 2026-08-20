package model

import "time"

// PlayerManagementSnapshot is a detached point-in-time view for Agent's
// private management plane. It contains no mutable maps, sessions, or packet
// senders and is therefore safe to serialize after releasing player locks.
type PlayerManagementSnapshot struct {
	CharacterID int       `json:"characterId"`
	UniqueID    uint32    `json:"uniqueId"`
	Name        string    `json:"name"`
	Level       int       `json:"level"`
	Gold        uint64    `json:"gold"`
	HP          int       `json:"hp"`
	MP          int       `json:"mp"`
	RegionID    int16     `json:"regionId"`
	PositionX   float32   `json:"positionX"`
	PositionY   float32   `json:"positionY"`
	PositionZ   float32   `json:"positionZ"`
	GameMaster  bool      `json:"gameMaster"`
	LastSeen    time.Time `json:"lastSeen"`
}

func (p *Player) ManagementSnapshot() PlayerManagementSnapshot {
	if p == nil {
		return PlayerManagementSnapshot{}
	}
	position := p.GetNavmeshPosition()
	p.Mutex.Lock()
	snapshot := PlayerManagementSnapshot{
		CharacterID: p.ID,
		Name:        p.CharName,
		Level:       p.Level,
		Gold:        p.Gold,
		HP:          p.BaseStats.HP,
		MP:          p.BaseStats.MP,
		GameMaster:  p.IsGm,
	}
	p.Mutex.Unlock()
	snapshot.UniqueID = p.GetUniqueID()
	snapshot.RegionID = position.Region.ID
	if position.Offset != nil {
		snapshot.PositionX = position.Offset.X
		snapshot.PositionY = position.Offset.Y
		snapshot.PositionZ = position.Offset.Z
	}
	if session := p.GetSession(); session != nil {
		snapshot.LastSeen = session.LastSeen().UTC()
	}
	return snapshot
}
