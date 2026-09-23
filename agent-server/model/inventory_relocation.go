package model

import (
	"errors"
	"fmt"
	"strings"
)

// InventoryRelocationAction describes the already-supported single-item
// relocation outcomes. It deliberately does not add split, merge, stack, or
// currency behaviour; those need a wider transaction kernel and captured
// protocol evidence.
type InventoryRelocationAction int

const (
	InventoryRelocationEquip   InventoryRelocationAction = InventoryRelocationAction(EquipItem)
	InventoryRelocationUnequip InventoryRelocationAction = InventoryRelocationAction(UnequipItem)
	InventoryRelocationMove    InventoryRelocationAction = InventoryRelocationAction(MoveItem)
	InventoryRelocationMerge   InventoryRelocationAction = 4
	InventoryRelocationSplit   InventoryRelocationAction = 5
)

var (
	ErrInventoryRelocationPlayerRequired  = errors.New("inventory relocation player is required")
	ErrInventoryRelocationSourceMissing   = errors.New("inventory relocation source item is missing")
	ErrInventoryRelocationSlotOutOfRange  = errors.New("inventory relocation slot is out of range")
	ErrInventoryRelocationSameSlot        = errors.New("inventory relocation source and target slots are the same")
	ErrInventoryRelocationItemIDRequired  = errors.New("inventory relocation item ID is required")
	ErrInventoryRelocationNotAllowed      = errors.New("inventory relocation is not allowed")
	ErrInventoryRelocationTargetOccupied  = errors.New("inventory relocation unequip target is occupied")
	ErrInventoryRelocationCommitterNeeded = errors.New("inventory relocation committer is required")
	ErrInventoryRelocationRuntimeConflict = errors.New("inventory relocation runtime state changed before commit")
	ErrInventoryRelocationInvalidPlan     = errors.New("inventory relocation plan is invalid")
)

// InventoryRelocationPlan is an immutable description of one existing item
// move, swap, equip, or unequip. SourceItemID and TargetItemID bind the plan
// to the exact runtime/database items observed before the transaction begins;
// TargetItemID is zero only when the target slot was empty.
//
// The packet-facing fields preserve the legacy equip/unequip notification
// contents without retaining a mutable Item value in the plan.
type InventoryRelocationPlan struct {
	SourceSlot byte
	TargetSlot byte

	SourceItemID int64
	TargetItemID int64

	Action InventoryRelocationAction

	SourceReferenceID uint32
	SourceOneHanded   bool
	SourceAmmunition  bool
	TransferQuantity  uint16
	SourceStackBefore int
	TargetStackBefore int

	// AdditionalMoves carries body items displaced by a paired equipment
	// transition. SR_GameServer's attach path explicitly handles slots 6 and
	// 7 as a unit; retaining these moves in the same plan keeps persistence and
	// runtime state atomic rather than leaving an incompatible off-hand item.
	AdditionalMoves []InventoryRelocationAdditionalMove
}

// PlanInventoryRelocationAmount extends ordinary relocation with the stock
// operation-0x00 quantity semantics used when one stack is dropped on another.
func PlanInventoryRelocationAmount(items map[byte]Item, sourceSlot, targetSlot byte, quantity uint16, player *Player) (InventoryRelocationPlan, error) {
	plan, err := PlanInventoryRelocation(items, sourceSlot, targetSlot, player)
	if err != nil || quantity == 0 {
		return plan, err
	}
	source, target := items[sourceSlot], items[targetSlot]
	if plan.TargetItemID == 0 {
		ref, exists := RefItems[source.GetRefObjectID()]
		// The stock client sends quantity 1 for ordinary equipment moves even
		// though equipment is not stackable. Preserve the normal relocation
		// plan; quantity semantics apply only to stackable items.
		if exists && ref.StackSize <= 1 {
			return plan, nil
		}
		if !exists || int(quantity) > source.StackSize {
			return InventoryRelocationPlan{}, fmt.Errorf("%w: invalid stack split quantity %d from %d", ErrInventoryRelocationNotAllowed, quantity, source.StackSize)
		}
		if int(quantity) == source.StackSize {
			return plan, nil
		}
		plan.Action = InventoryRelocationSplit
		plan.TransferQuantity = quantity
		plan.SourceStackBefore = source.StackSize
		return plan, nil
	}
	if source.GetRefObjectID() != target.GetRefObjectID() || source.StackSize < 1 || target.StackSize < 1 {
		return plan, nil
	}
	ref, exists := RefItems[source.GetRefObjectID()]
	if !exists || ref.StackSize <= 1 || int(quantity) > source.StackSize || target.StackSize+int(quantity) > ref.StackSize {
		return InventoryRelocationPlan{}, fmt.Errorf("%w: invalid stack merge %d + %d (max %d)", ErrInventoryRelocationNotAllowed, target.StackSize, quantity, ref.StackSize)
	}
	plan.Action = InventoryRelocationMerge
	plan.TransferQuantity = quantity
	plan.SourceStackBefore = source.StackSize
	plan.TargetStackBefore = target.StackSize
	return plan, nil
}

type InventoryRelocationAdditionalMove struct {
	SourceSlot        byte
	TargetSlot        byte
	SourceItemID      int64
	SourceReferenceID uint32
	SourceAmmunition  bool
}

// InventoryRelocationCommit persists one validated plan. CommitRelocation
// calls it while the owning inventory is locked, then applies the plan to the
// runtime map only when this function returns nil.
type InventoryRelocationCommit func(InventoryRelocationPlan) error

// PlanInventoryRelocation validates and describes a single existing
// move/swap/equip/unequip without changing items. It intentionally accepts a
// map rather than an Inventory so it remains a pure planning helper that is
// easy to exercise independently of database and world state.
func PlanInventoryRelocation(items map[byte]Item, sourceSlot, targetSlot byte, player *Player) (InventoryRelocationPlan, error) {
	if player == nil {
		return InventoryRelocationPlan{}, ErrInventoryRelocationPlayerRequired
	}
	if err := validateInventoryRelocationSlotForPlayer(sourceSlot, player); err != nil {
		return InventoryRelocationPlan{}, err
	}
	if err := validateInventoryRelocationSlotForPlayer(targetSlot, player); err != nil {
		return InventoryRelocationPlan{}, err
	}
	if sourceSlot == targetSlot {
		return InventoryRelocationPlan{}, ErrInventoryRelocationSameSlot
	}

	sourceItem, sourceExists := items[sourceSlot]
	if !sourceExists {
		return InventoryRelocationPlan{}, fmt.Errorf("%w: slot %d", ErrInventoryRelocationSourceMissing, sourceSlot)
	}
	if sourceItem.ID <= 0 {
		return InventoryRelocationPlan{}, fmt.Errorf("%w: source slot %d", ErrInventoryRelocationItemIDRequired, sourceSlot)
	}

	targetItem, targetExists := items[targetSlot]
	if targetExists && targetItem.ID <= 0 {
		return InventoryRelocationPlan{}, fmt.Errorf("%w: target slot %d", ErrInventoryRelocationItemIDRequired, targetSlot)
	}

	plan := InventoryRelocationPlan{
		SourceSlot:        sourceSlot,
		TargetSlot:        targetSlot,
		SourceItemID:      int64(sourceItem.ID),
		Action:            InventoryRelocationMove,
		SourceReferenceID: sourceItem.GetRefObjectID(),
		SourceOneHanded:   sourceItem.IsOneHandedWeapon(),
		SourceAmmunition:  sourceItem.IsArrow() || sourceItem.IsBolt(),
	}
	if targetExists {
		plan.TargetItemID = int64(targetItem.ID)
	}

	// Keep the established equip rules, but evaluate them against the supplied
	// map only. This temporary Inventory has no relationship to the player's
	// runtime map and performs no writes.
	rules := NewInventory(items)
	if targetSlot <= SlotRightRing {
		if canEquipAtSlot(&rules, sourceItem, targetSlot, player) {
			plan.Action = InventoryRelocationEquip
			if targetSlot == SlotPrimaryWeapon && !sourceItem.IsOneHandedWeapon() {
				if secondary, occupied := items[SlotSecondaryWeapon]; occupied {
					destination, found := pairedDisplacementDestination(items, player, sourceSlot, targetSlot, targetExists)
					if !found {
						return InventoryRelocationPlan{}, fmt.Errorf("%w: paired equipment requires a free normal slot", ErrInventoryRelocationTargetOccupied)
					}
					plan.AdditionalMoves = []InventoryRelocationAdditionalMove{{
						SourceSlot:        SlotSecondaryWeapon,
						TargetSlot:        destination,
						SourceItemID:      int64(secondary.ID),
						SourceReferenceID: secondary.GetRefObjectID(),
						SourceAmmunition:  secondary.IsArrow() || secondary.IsBolt(),
					}}
				}
			}
			return plan, nil
		}
		return InventoryRelocationPlan{}, fmt.Errorf("%w: item %d cannot equip slot %d", ErrInventoryRelocationNotAllowed, sourceItem.ID, targetSlot)
	}

	if sourceSlot <= SlotRightRing && sourceItem.IsEquipment() {
		if targetExists {
			return InventoryRelocationPlan{}, fmt.Errorf("%w: slot %d", ErrInventoryRelocationTargetOccupied, targetSlot)
		}
		plan.Action = InventoryRelocationUnequip
	}
	return plan, nil
}

func pairedDisplacementDestination(items map[byte]Item, player *Player, sourceSlot, targetSlot byte, targetOccupied bool) (byte, bool) {
	// When the primary target is empty, the incoming item vacates its normal
	// source slot. Reusing it matches the storage-manipulator's capacity-aware
	// relocation rather than requiring an unrelated extra free slot.
	if !targetOccupied && sourceSlot >= InventoryEquipmentSlots {
		return sourceSlot, true
	}
	for slot := InventoryEquipmentSlots; slot < player.GetInventorySize(); slot++ {
		if slot == sourceSlot || slot == targetSlot {
			continue
		}
		if _, occupied := items[slot]; !occupied {
			return slot, true
		}
	}
	return 0, false
}

func canEquipAtSlot(inventory *Inventory, item Item, targetSlot byte, player *Player) bool {
	switch targetSlot {
	case SlotPrimaryWeapon:
		return inventory.canEquipPrimaryWeapon(item, player)
	case SlotSecondaryWeapon:
		return inventory.canEquipSecondaryWeapon(item, player)
	case SlotHelmet:
		return inventory.canEquipHelmet(item, player)
	case SlotChest:
		return inventory.canEquipChest(item, player)
	case SlotShoulder:
		return inventory.canEquipShoulder(item, player)
	case SlotGlove:
		return inventory.canEquipGloves(item, player)
	case SlotPants:
		return inventory.canEquipPants(item, player)
	case SlotBoots:
		return inventory.canEquipBoots(item, player)
	case SlotEarring:
		return inventory.canEquipEarring(item, player)
	case SlotNecklace:
		return inventory.canEquipNecklace(item, player)
	case SlotLeftRing, SlotRightRing:
		return inventory.canEquipRing(item, player)
	case SlotExtra:
		return canEquipJobSuit(item, player)
	default:
		return false
	}
}

func canEquipJobSuit(item Item, player *Player) bool {
	if player == nil {
		return false
	}
	ref, ok := RefItems[item.GetRefObjectID()]
	if !ok {
		return false
	}
	code := strings.ToUpper(strings.TrimSpace(ref.CodeName))
	if !strings.Contains(code, "_TRADE_") {
		return false
	}
	switch player.GetJob().Type {
	case PlayerJobTrader:
		return strings.Contains(code, "_TRADE_TRADER_")
	case PlayerJobThief:
		return strings.Contains(code, "_TRADE_THIEF_")
	case PlayerJobHunter:
		return strings.Contains(code, "_TRADE_HUNTER_")
	default:
		return false
	}
}

// HasEquippedJobSuit reports whether slot 8 contains job clothes matching the
// player's selected trader, thief, or hunter role.
func HasEquippedJobSuit(player *Player) bool {
	if player == nil {
		return false
	}
	item, equipped := player.Inventory.Items[SlotExtra]
	return equipped && canEquipJobSuit(item, player)
}

// HasActiveJobSuit reports whether slot 8 contains job clothes matching the
// player's active trader or thief role. Trade transports are not available to
// hunters or to characters whose job clothes are only present in the bag.
func HasActiveJobSuit(player *Player) bool {
	if player == nil {
		return false
	}
	jobType := player.GetJob().Type
	return (jobType == PlayerJobTrader || jobType == PlayerJobThief) && HasEquippedJobSuit(player)
}

// ApplyInventoryRelocation applies one previously-built plan to items. It
// validates every affected slot before its first write, so a stale runtime map
// remains untouched rather than becoming partially swapped.
func ApplyInventoryRelocation(items map[byte]Item, plan InventoryRelocationPlan) error {
	if err := validateInventoryRelocationPlan(plan); err != nil {
		return err
	}
	next := make(map[byte]Item, len(items))
	for slot, item := range items {
		next[slot] = item
	}

	sourceItem, sourceExists := next[plan.SourceSlot]
	if !sourceExists || int64(sourceItem.ID) != plan.SourceItemID {
		return fmt.Errorf("%w: source slot %d", ErrInventoryRelocationRuntimeConflict, plan.SourceSlot)
	}

	targetItem, targetExists := next[plan.TargetSlot]
	if plan.TargetItemID == 0 {
		if targetExists {
			return fmt.Errorf("%w: target slot %d became occupied", ErrInventoryRelocationRuntimeConflict, plan.TargetSlot)
		}
	} else if !targetExists || int64(targetItem.ID) != plan.TargetItemID {
		return fmt.Errorf("%w: target slot %d", ErrInventoryRelocationRuntimeConflict, plan.TargetSlot)
	}

	if plan.Action == InventoryRelocationMerge {
		if sourceItem.StackSize != plan.SourceStackBefore || targetItem.StackSize != plan.TargetStackBefore {
			return fmt.Errorf("%w: stack counts changed", ErrInventoryRelocationRuntimeConflict)
		}
		targetItem.StackSize += int(plan.TransferQuantity)
		next[plan.TargetSlot] = targetItem
		if remaining := sourceItem.StackSize - int(plan.TransferQuantity); remaining > 0 {
			sourceItem.StackSize = remaining
			next[plan.SourceSlot] = sourceItem
		} else {
			delete(next, plan.SourceSlot)
		}
	} else {
		next[plan.TargetSlot] = sourceItem
		if targetExists {
			next[plan.SourceSlot] = targetItem
		} else {
			delete(next, plan.SourceSlot)
		}
	}
	for _, move := range plan.AdditionalMoves {
		item, exists := next[move.SourceSlot]
		if !exists || int64(item.ID) != move.SourceItemID {
			return fmt.Errorf("%w: displaced source slot %d", ErrInventoryRelocationRuntimeConflict, move.SourceSlot)
		}
		if _, occupied := next[move.TargetSlot]; occupied {
			return fmt.Errorf("%w: displaced target slot %d", ErrInventoryRelocationRuntimeConflict, move.TargetSlot)
		}
		next[move.TargetSlot] = item
		delete(next, move.SourceSlot)
	}
	for slot := range items {
		delete(items, slot)
	}
	for slot, item := range next {
		items[slot] = item
	}
	return nil
}

// CommitRelocation is the runtime half of a database-first relocation. It
// serializes only this character's inventory, builds a pure plan, invokes the
// supplied persistence transaction, and mutates the runtime map only after a
// successful commit. A database failure therefore leaves the visible player
// inventory exactly as it was before the packet arrived.
func (i *Inventory) CommitRelocation(player *Player, sourceSlot, targetSlot byte, commit InventoryRelocationCommit) (InventoryRelocationPlan, error) {
	return i.CommitRelocationAmount(player, sourceSlot, targetSlot, 0, commit)
}

func (i *Inventory) CommitRelocationAmount(player *Player, sourceSlot, targetSlot byte, quantity uint16, commit InventoryRelocationCommit) (InventoryRelocationPlan, error) {
	if i == nil {
		return InventoryRelocationPlan{}, ErrInventoryRelocationRuntimeConflict
	}
	if commit == nil {
		return InventoryRelocationPlan{}, ErrInventoryRelocationCommitterNeeded
	}

	mutex := i.synchronizationMutex()
	mutex.Lock()
	defer mutex.Unlock()

	plan, err := PlanInventoryRelocationAmount(i.Items, sourceSlot, targetSlot, quantity, player)
	if err != nil {
		return InventoryRelocationPlan{}, err
	}
	if err := commit(plan); err != nil {
		return InventoryRelocationPlan{}, err
	}
	// A split creates a new persistent item identity. The handler reloads the
	// committed snapshot from the repository before releasing the economy gate.
	if plan.Action == InventoryRelocationSplit {
		return plan, nil
	}
	if err := ApplyInventoryRelocation(i.Items, plan); err != nil {
		return InventoryRelocationPlan{}, err
	}
	return plan, nil
}

func validateInventoryRelocationSlot(slot byte) error {
	if slot >= MaxInventoryWireSize {
		return fmt.Errorf("%w: %d (wire maximum %d)", ErrInventoryRelocationSlotOutOfRange, slot, MaxNormalInventorySlot)
	}
	return nil
}

// validateInventoryRelocationSlotForPlayer applies SR_GameServer's second
// bound after the wire range: a slot must exist in this character's current
// inventory. Body slots are always within the character inventory; normal
// slots require the runtime capacity to include them.
func validateInventoryRelocationSlotForPlayer(slot byte, player *Player) error {
	if err := validateInventoryRelocationSlot(slot); err != nil {
		return err
	}
	if capacity := player.GetInventorySize(); capacity < InventoryEquipmentSlots || capacity > MaxInventoryWireSize || slot >= capacity {
		return fmt.Errorf("%w: %d (character capacity %d)", ErrInventoryRelocationSlotOutOfRange, slot, capacity)
	}
	return nil
}

func validateInventoryRelocationPlan(plan InventoryRelocationPlan) error {
	if err := validateInventoryRelocationSlot(plan.SourceSlot); err != nil {
		return err
	}
	if err := validateInventoryRelocationSlot(plan.TargetSlot); err != nil {
		return err
	}
	if plan.SourceSlot == plan.TargetSlot || plan.SourceItemID <= 0 || plan.TargetItemID < 0 {
		return ErrInventoryRelocationInvalidPlan
	}
	switch plan.Action {
	case InventoryRelocationEquip, InventoryRelocationUnequip, InventoryRelocationMove:
		if plan.TransferQuantity != 0 || plan.SourceStackBefore != 0 || plan.TargetStackBefore != 0 {
			return ErrInventoryRelocationInvalidPlan
		}
	case InventoryRelocationMerge:
		if plan.TargetItemID <= 0 || plan.TransferQuantity == 0 || plan.SourceStackBefore < int(plan.TransferQuantity) || plan.TargetStackBefore < 1 {
			return ErrInventoryRelocationInvalidPlan
		}
	case InventoryRelocationSplit:
		if plan.TargetItemID != 0 || plan.TransferQuantity == 0 || plan.SourceStackBefore <= int(plan.TransferQuantity) || plan.TargetStackBefore != 0 {
			return ErrInventoryRelocationInvalidPlan
		}
	default:
		return ErrInventoryRelocationInvalidPlan
	}
	seenSources := map[byte]struct{}{plan.SourceSlot: {}}
	seenTargets := map[byte]struct{}{plan.TargetSlot: {}}
	for _, move := range plan.AdditionalMoves {
		if err := validateInventoryRelocationSlot(move.SourceSlot); err != nil {
			return err
		}
		if err := validateInventoryRelocationSlot(move.TargetSlot); err != nil {
			return err
		}
		if move.SourceItemID <= 0 {
			return ErrInventoryRelocationInvalidPlan
		}
		if _, duplicate := seenSources[move.SourceSlot]; duplicate {
			return ErrInventoryRelocationInvalidPlan
		}
		if _, duplicate := seenTargets[move.TargetSlot]; duplicate {
			return ErrInventoryRelocationInvalidPlan
		}
		seenSources[move.SourceSlot] = struct{}{}
		seenTargets[move.TargetSlot] = struct{}{}
	}
	return nil
}
