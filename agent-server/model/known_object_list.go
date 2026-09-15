package model

import "sync"

type IKnownObjectList interface {
	AddObject(object ISRObject) bool
	RemoveObject(object ISRObject) bool
	GetKnownObjects() map[uint32]ISRObject
	Knows(object ISRObject) bool
}

type KnownObjectList struct {
	KnownObjects map[uint32]ISRObject
	Owner        ISRObject
	mutex        sync.Mutex
}

func NewKnownObjectList(owner ISRObject) *KnownObjectList {
	return &KnownObjectList{
		KnownObjects: make(map[uint32]ISRObject),
		Owner:        owner,
		mutex:        sync.Mutex{},
	}
}

func (k *KnownObjectList) AddObject(object ISRObject) bool {
	k.mutex.Lock()
	defer k.mutex.Unlock()
	if _, exists := k.KnownObjects[object.GetUniqueID()]; object.GetUniqueID() == k.Owner.GetUniqueID() || exists {
		return false
	}

	k.KnownObjects[object.GetUniqueID()] = object
	return true
}

func (k *KnownObjectList) RemoveObject(object ISRObject) bool {
	k.mutex.Lock()
	defer k.mutex.Unlock()
	if _, exists := k.KnownObjects[object.GetUniqueID()]; object.GetUniqueID() != k.Owner.GetUniqueID() && exists {
		delete(k.KnownObjects, object.GetUniqueID())
		return true
	}

	return false
}

func (k *KnownObjectList) GetKnownObjects() map[uint32]ISRObject {
	k.mutex.Lock()
	defer k.mutex.Unlock()
	// Callers use this collection for visibility and broadcast scans. Return a
	// detached map so they cannot mutate the authoritative list or race an
	// add/remove operation after this lock is released.
	objects := make(map[uint32]ISRObject, len(k.KnownObjects))
	for uniqueID, object := range k.KnownObjects {
		objects[uniqueID] = object
	}
	return objects
}

// AppendKnownObjects appends a locked snapshot to caller-owned storage. Hot
// visibility paths use it to reuse buffers without exposing the authoritative
// map; GetKnownObjects remains available when a detached map is required.
func (k *KnownObjectList) AppendKnownObjects(destination []ISRObject) []ISRObject {
	if k == nil {
		return destination
	}
	k.mutex.Lock()
	defer k.mutex.Unlock()
	for _, object := range k.KnownObjects {
		destination = append(destination, object)
	}
	return destination
}

func (k *KnownObjectList) Knows(object ISRObject) bool {
	k.mutex.Lock()
	defer k.mutex.Unlock()
	_, exists := k.KnownObjects[object.GetUniqueID()]
	return exists
}
