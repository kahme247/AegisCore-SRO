package model

import "sort"

// SortedInventorySlots returns the present inventory slots in ascending numeric
// order. Packet serializers use this order both for layouts that carry an
// explicit slot and for legacy appearance lists that only carry item data.
//
// Keeping the ordering at this boundary prevents Go map iteration order from
// changing otherwise identical character packets. It deliberately does not
// infer any additional wire fields or filter items: callers retain their
// existing payload layout and item set.
func SortedInventorySlots(items map[byte]Item) []byte {
	slots := make([]byte, 0, len(items))
	for slot := range items {
		slots = append(slots, slot)
	}
	sort.Slice(slots, func(left, right int) bool {
		return slots[left] < slots[right]
	})
	return slots
}
