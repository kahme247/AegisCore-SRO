package model

import (
	"sync"
	"time"

	"github.com/ferdoran/go-sro/agent-server/navmeshv2"
	"github.com/sirupsen/logrus"
)

type ISRObject interface {
	GetType() string
	GetTypeInfo() TypeInfo
	SetTypeInfo(info TypeInfo)
	SetPosition(pos Position)
	SetNavmeshPosition(pos navmeshv2.RtNavmeshPosition)
	GetPosition() Position
	GetNavmeshPosition() navmeshv2.RtNavmeshPosition
	GetUniqueID() uint32
	SetUniqueID(uniqueId uint32)
	GetRefObjectID() uint32
	GetKnownObjectList() IKnownObjectList
	GetName() string
	GetMovementData() *MovementData
	BeginMovement(data MovementData) MovementData
	CommitMovementStep(generation uint64, position navmeshv2.RtNavmeshPosition, updateTime time.Time) bool
	FinalizeMovement(generation uint64, position navmeshv2.RtNavmeshPosition, reason string) bool
	CancelMovement(position navmeshv2.RtNavmeshPosition, reason string) uint64
	GetMovementGeneration() uint64
	GetLastMovementStopReason() string
}

type SRObject struct {
	navmeshv2.RtNavmeshPosition
	Position
	TypeInfo
	movementData           *MovementData
	KnownObjectList        IKnownObjectList
	UniqueID               uint32
	RefObjectID            uint32
	Type                   string
	mutex                  *sync.RWMutex
	Name                   string
	movementGeneration     uint64
	lastMovementStopReason string
}

// fallbackSRObjectMutex keeps zero-value SRObject literals safe. A large
// portion of the model is still assembled with struct literals (including
// copied Item values), so embedding a sync.RWMutex made otherwise ordinary
// value copies fail vet. Constructors can use NewSRObject when they need a
// dedicated lock; zero values intentionally share this fallback instead of
// retaining a per-address lock forever.
var fallbackSRObjectMutex = &sync.RWMutex{}

// NewSRObject constructs an SRObject with a dedicated lock. The zero value is
// also valid and uses the package fallback lock through mutexForObject.
func NewSRObject() SRObject {
	return SRObject{mutex: &sync.RWMutex{}}
}

func (o *SRObject) mutexForObject() *sync.RWMutex {
	if o != nil && o.mutex != nil {
		return o.mutex
	}
	return fallbackSRObjectMutex
}

func cloneNavmeshPosition(position navmeshv2.RtNavmeshPosition) navmeshv2.RtNavmeshPosition {
	cloned := position
	if position.Offset != nil {
		cloned.Offset = position.Offset.Clone()
	}
	return cloned
}

func cloneMovementData(data MovementData) MovementData {
	cloned := data
	cloned.TargetPosition = cloneNavmeshPosition(data.TargetPosition)
	return cloned
}

func (o *SRObject) GetRefObjectID() uint32 {
	return o.RefObjectID
}

func (o *SRObject) GetPosition() (position Position) {
	mutex := o.mutexForObject()
	mutex.RLock()
	defer mutex.RUnlock()
	position = o.Position
	return
}

func (o *SRObject) SetNavmeshPosition(newPosition navmeshv2.RtNavmeshPosition) {
	mutex := o.mutexForObject()
	mutex.Lock()
	defer mutex.Unlock()
	logrus.Debugf("Setting position to %s", newPosition.String())
	o.RtNavmeshPosition = cloneNavmeshPosition(newPosition)
}

func (o *SRObject) GetNavmeshPosition() (position navmeshv2.RtNavmeshPosition) {
	mutex := o.mutexForObject()
	mutex.RLock()
	defer mutex.RUnlock()
	position = cloneNavmeshPosition(o.RtNavmeshPosition)
	return
}

func (o *SRObject) SetPosition(newPosition Position) {
	mutex := o.mutexForObject()
	mutex.Lock()
	defer mutex.Unlock()

	//if oldReg := o.GetPosition().Region; oldReg != nil && oldReg.ID != newPosition.Region.ID {
	//	o.Region.RemoveVisibleObject(o)
	//}
	o.Position = newPosition
	//o.Region.AddVisibleObject(o)
}

func (o *SRObject) GetUniqueID() uint32 {
	return o.UniqueID
}

func (o *SRObject) SetUniqueID(uniqueID uint32) {
	mutex := o.mutexForObject()
	mutex.Lock()
	defer mutex.Unlock()
	o.UniqueID = uniqueID
}

func (o *SRObject) GetType() string {
	return "SRObject"
}

func (o *SRObject) GetTypeInfo() TypeInfo {
	return o.TypeInfo
}

func (o *SRObject) SetTypeInfo(info TypeInfo) {
	mutex := o.mutexForObject()
	mutex.Lock()
	defer mutex.Unlock()
	o.TypeInfo = info
}

func (o *SRObject) GetKnownObjectList() IKnownObjectList {
	mutex := o.mutexForObject()
	mutex.RLock()
	defer mutex.RUnlock()
	return o.KnownObjectList
}

func (o *SRObject) GetName() string {
	return o.Name
}

func (o *SRObject) GetMovementData() *MovementData {
	mutex := o.mutexForObject()
	mutex.RLock()
	defer mutex.RUnlock()
	if o.movementData == nil {
		return nil
	}
	cloned := cloneMovementData(*o.movementData)
	return &cloned
}

func (o *SRObject) BeginMovement(data MovementData) MovementData {
	mutex := o.mutexForObject()
	mutex.Lock()
	defer mutex.Unlock()
	o.movementGeneration++
	data.Generation = o.movementGeneration
	o.lastMovementStopReason = ""
	cloned := cloneMovementData(data)
	o.movementData = &cloned
	return cloneMovementData(cloned)
}

func (o *SRObject) CommitMovementStep(generation uint64, position navmeshv2.RtNavmeshPosition, updateTime time.Time) bool {
	mutex := o.mutexForObject()
	mutex.Lock()
	defer mutex.Unlock()
	if o.movementData == nil || o.movementData.Generation != generation {
		return false
	}
	o.RtNavmeshPosition = cloneNavmeshPosition(position)
	o.movementData.UpdateTime = updateTime
	return true
}

func (o *SRObject) FinalizeMovement(generation uint64, position navmeshv2.RtNavmeshPosition, reason string) bool {
	mutex := o.mutexForObject()
	mutex.Lock()
	defer mutex.Unlock()
	if o.movementData == nil || o.movementData.Generation != generation {
		return false
	}
	o.RtNavmeshPosition = cloneNavmeshPosition(position)
	o.movementData = nil
	o.lastMovementStopReason = reason
	return true
}

func (o *SRObject) CancelMovement(position navmeshv2.RtNavmeshPosition, reason string) uint64 {
	mutex := o.mutexForObject()
	mutex.Lock()
	defer mutex.Unlock()
	o.movementGeneration++
	o.RtNavmeshPosition = cloneNavmeshPosition(position)
	o.movementData = nil
	o.lastMovementStopReason = reason
	return o.movementGeneration
}

func (o *SRObject) GetMovementGeneration() uint64 {
	mutex := o.mutexForObject()
	mutex.RLock()
	defer mutex.RUnlock()
	return o.movementGeneration
}

func (o *SRObject) GetLastMovementStopReason() string {
	mutex := o.mutexForObject()
	mutex.RLock()
	defer mutex.RUnlock()
	return o.lastMovementStopReason
}
