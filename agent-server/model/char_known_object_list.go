package model

import (
	"github.com/sirupsen/logrus"
	"sync"
)

type ICharKnownObjectList interface {
	IKnownObjectList
	GetObjectsToSpawn() map[uint32]ISRObject
	GetObjectsToDespawn() map[uint32]ISRObject
	MarkSpawned(map[uint32]ISRObject)
	MarkDespawned(map[uint32]ISRObject)
	ClientKnows(uint32) bool
	ResetClientWorld()
}

type CharKnownObjectList struct {
	KnownObjectList
	ObjectsToSpawn   map[uint32]ISRObject
	ObjectsToDespawn map[uint32]ISRObject
	ClientObjects    map[uint32]ISRObject
	mutex            sync.Mutex
}

func (k *CharKnownObjectList) AddObject(object ISRObject) bool {
	k.mutex.Lock()
	defer k.mutex.Unlock()
	logrus.Debugf("adding object %d to CharKnownObjectList with type %s", object.GetUniqueID(), object.GetType())
	if k.KnownObjectList.AddObject(object) {
		uid := object.GetUniqueID()
		if _, exists := k.ObjectsToDespawn[uid]; exists {
			// The removal was not delivered yet, so the client still knows this
			// object. Cancel it without sending a duplicate spawn.
			delete(k.ObjectsToDespawn, object.GetUniqueID())
			return true
		}

		if _, delivered := k.ClientObjects[uid]; !delivered {
			k.ObjectsToSpawn[uid] = object
			return true
		}

	}

	return false
}

func (k *CharKnownObjectList) RemoveObject(object ISRObject) bool {
	k.mutex.Lock()
	defer k.mutex.Unlock()
	logrus.Debugf("removing object %d from CharKnownObjectList with type %s", object.GetUniqueID(), object.GetType())
	if k.KnownObjectList.RemoveObject(object) {
		uid := object.GetUniqueID()
		if _, exists := k.ObjectsToSpawn[uid]; exists {
			// The spawn was never delivered, so the client must not receive a
			// despawn for this object.
			delete(k.ObjectsToSpawn, uid)
			return true
		}

		if _, delivered := k.ClientObjects[uid]; delivered {
			k.ObjectsToDespawn[uid] = object
			return true
		}

	}

	return false
}

func (k *CharKnownObjectList) GetObjectsToSpawn() map[uint32]ISRObject {
	k.mutex.Lock()
	defer k.mutex.Unlock()

	m := k.ObjectsToSpawn
	k.ObjectsToSpawn = make(map[uint32]ISRObject)
	return m
}

func (k *CharKnownObjectList) GetObjectsToDespawn() map[uint32]ISRObject {
	k.mutex.Lock()
	defer k.mutex.Unlock()

	m := k.ObjectsToDespawn
	k.ObjectsToDespawn = make(map[uint32]ISRObject)
	return m
}

func (k *CharKnownObjectList) MarkSpawned(objects map[uint32]ISRObject) {
	k.mutex.Lock()
	defer k.mutex.Unlock()
	for uid, object := range objects {
		k.ClientObjects[uid] = object
	}
}

func (k *CharKnownObjectList) MarkDespawned(objects map[uint32]ISRObject) {
	k.mutex.Lock()
	defer k.mutex.Unlock()
	for uid := range objects {
		delete(k.ClientObjects, uid)
	}
}

func (k *CharKnownObjectList) ClientKnows(uid uint32) bool {
	k.mutex.Lock()
	defer k.mutex.Unlock()
	_, exists := k.ClientObjects[uid]
	return exists
}

// ResetClientWorld forgets every visibility association and delivery queue
// without producing despawns. The Agent game-reset packet makes the client
// discard its complete object table during teleport loading, so replaying
// removals for the previous area would reference UIDs the client no longer
// owns.
func (k *CharKnownObjectList) ResetClientWorld() {
	k.mutex.Lock()
	defer k.mutex.Unlock()

	k.KnownObjectList.mutex.Lock()
	k.KnownObjectList.KnownObjects = make(map[uint32]ISRObject)
	k.KnownObjectList.mutex.Unlock()
	k.ObjectsToSpawn = make(map[uint32]ISRObject)
	k.ObjectsToDespawn = make(map[uint32]ISRObject)
	k.ClientObjects = make(map[uint32]ISRObject)
}

func NewCharKnownObjectList(owner ISRObject) *CharKnownObjectList {
	return &CharKnownObjectList{
		KnownObjectList: KnownObjectList{
			KnownObjects: make(map[uint32]ISRObject),
			Owner:        owner,
			mutex:        sync.Mutex{},
		},
		ObjectsToSpawn:   make(map[uint32]ISRObject),
		ObjectsToDespawn: make(map[uint32]ISRObject),
		ClientObjects:    make(map[uint32]ISRObject),
		mutex:            sync.Mutex{},
	}
}

func (k *CharKnownObjectList) GetKnownObjects() map[uint32]ISRObject {
	return k.KnownObjectList.GetKnownObjects()
}
