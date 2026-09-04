package model

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	alchemyRondoResolve = "ITEM_ETC_ARCHEMY_RONDO_01"
	alchemyRondoEquip   = "ITEM_ETC_ARCHEMY_RONDO_02"
)

type alchemyRecipePart struct {
	code   string
	amount int
}

var dismantleElementPairs = [12][2]int{{0, 1}, {0, 2}, {0, 3}, {1, 0}, {1, 2}, {1, 3}, {2, 0}, {2, 1}, {2, 3}, {3, 0}, {3, 1}, {3, 2}}

var dismantleElementNames = [4]string{"EARTH", "WATER", "FIRE", "WIND"}

func (r *SQLAlchemyRepository) ResolveMaterial(ctx context.Context, characterID int64, userID uint32, sourceSlot byte) (result AlchemyMakeResult, err error) {
	tx, inventory, err := r.lockOwnedAlchemyInventory(ctx, characterID, userID)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback() }()
	source, ok := inventory.Items[sourceSlot]
	ref, refOK := RefItems[source.RefObjectID]
	if !ok || !refOK || sourceSlot < InventoryEquipmentSlots || source.StackSize < 1 {
		return result, ErrAlchemyConflict
	}
	if ref.TypeID1 != 3 || ref.TypeID2 != 3 || ref.TypeID3 != 11 || ref.TypeID4 != 4 {
		return result, ErrAlchemyUnsupported
	}
	parts := alchemyRecipeParts(ref)
	if len(parts) == 0 {
		return result, ErrAlchemyUnsupported
	}
	operationValue := source.StackSize
	maxInt := int(^uint(0) >> 1)
	rondoPerOperation := ref.Price/1000 + 1
	if rondoPerOperation > uint64(maxInt/operationValue) {
		return result, ErrAlchemyConflict
	}
	requiredRondo := int(rondoPerOperation) * operationValue
	if requiredRondo < 1 {
		requiredRondo = 1
	}
	if err = consumeAlchemyCode(ctx, tx, characterID, inventory, alchemyRondoResolve, requiredRondo, &result.Materials); err != nil {
		return result, err
	}
	if err = consumeAlchemyAmount(ctx, tx, characterID, inventory, sourceSlot, source, operationValue, &result.Materials); err != nil {
		return result, err
	}
	for _, part := range parts {
		outputRef, found := alchemyRefByCode(part.code)
		if !found || part.amount <= 0 || part.amount > maxInt/operationValue {
			return result, ErrAlchemyUnsupported
		}
		_, addErr := addAlchemyOutput(ctx, tx, characterID, inventory, outputRef, part.amount*operationValue)
		if addErr != nil {
			return result, addErr
		}
	}
	result.ResponseByte = byte(requiredRondo)
	result.ProducedValue = uint32(operationValue)
	return finishAlchemyMake(ctx, tx, characterID, result)
}

func (r *SQLAlchemyRepository) CompoundMaterial(ctx context.Context, characterID int64, userID uint32, slots []byte) (result AlchemyMakeResult, err error) {
	if len(slots) < 2 {
		return result, ErrAlchemyInvalidRequest
	}
	tx, inventory, err := r.lockOwnedAlchemyInventory(ctx, characterID, userID)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback() }()
	recipe, ok := inventory.Items[slots[0]]
	ref, refOK := RefItems[recipe.RefObjectID]
	if !ok || !refOK {
		return result, ErrAlchemyConflict
	}
	if ref.TypeID1 != 3 || ref.TypeID2 != 3 || ref.TypeID3 != 11 || ref.TypeID4 != 3 {
		return result, ErrAlchemyUnsupported
	}
	parts := alchemyRecipeParts(ref)
	outputCode := strings.TrimSpace(ref.Param5Desc)
	outputRef, outputOK := alchemyRefByCode(outputCode)
	if len(parts) == 0 || !outputOK {
		return result, ErrAlchemyUnsupported
	}
	allowed := make(map[byte]struct{}, len(slots)-1)
	seen := make(map[byte]struct{}, len(slots))
	for index, slot := range slots {
		if _, duplicate := seen[slot]; duplicate || slot < InventoryEquipmentSlots {
			return result, ErrAlchemyInvalidRequest
		}
		seen[slot] = struct{}{}
		if index != 0 {
			allowed[slot] = struct{}{}
		}
	}
	for _, part := range parts {
		if err = consumeAlchemyCodeFromSlots(ctx, tx, characterID, inventory, allowed, part.code, part.amount, &result.Materials); err != nil {
			return result, err
		}
	}
	if err = consumeAlchemyAmount(ctx, tx, characterID, inventory, slots[0], recipe, 1, &result.Materials); err != nil {
		return result, err
	}
	_, err = addAlchemyOutput(ctx, tx, characterID, inventory, outputRef, 1)
	if err != nil {
		return result, err
	}
	result.ProducedValue = 1
	result.ResponseByte = slots[0]
	return finishAlchemyMake(ctx, tx, characterID, result)
}

func (r *SQLAlchemyRepository) DismantleEquipment(ctx context.Context, characterID int64, userID uint32, slots []byte) (result AlchemyResult, err error) {
	if len(slots) == 0 || len(slots) > 9 {
		return result, ErrAlchemyInvalidRequest
	}
	tx, inventory, err := r.lockOwnedAlchemyInventory(ctx, characterID, userID)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback() }()
	seen := make(map[byte]struct{}, len(slots))
	requiredRondo := 0
	type plannedOutput struct {
		ref    RefItem
		amount int
	}
	outputs := make([]plannedOutput, 0, len(slots)*4)
	for _, slot := range slots {
		item, exists := inventory.Items[slot]
		ref, refExists := RefItems[item.RefObjectID]
		if _, duplicate := seen[slot]; duplicate || slot < InventoryEquipmentSlots || !exists || !refExists || !ref.IsEquipment() || ref.Degree() == 0 {
			return result, ErrAlchemyConflict
		}
		seen[slot] = struct{}{}
		value := ref.Price
		requiredRondo += int(value/20000) + 1
		grade := ref.Degree() - 1
		pairA := dismantleElementPairs[alchemyResidue(r.roll, 12)]
		pairB := dismantleElementPairs[alchemyResidue(r.roll, 12)]
		selectors := [4]int{pairA[0], pairA[1], pairB[0], pairB[1]}
		for outputIndex, selector := range selectors {
			base := 10
			scale := 0.020
			if outputIndex%2 == 0 {
				base, scale = 15, 0.025
			}
			amount := int(math.RoundToEven(((float64(alchemyResidue(r.roll, 6)+base)/100 + float64(item.OptLevel)*scale) * float64(value) / float64(dismantleGradeDivisors[grade])) + 1))
			if amount < 1 {
				amount = 1
			}
			code := fmt.Sprintf("ITEM_ETC_ARCHEMY_ELEMENT_%s_%02d", dismantleElementNames[selector], ref.Degree())
			outputRef, found := alchemyRefByCode(code)
			if !found {
				return result, ErrAlchemyUnsupported
			}
			outputs = append(outputs, plannedOutput{ref: outputRef, amount: amount})
		}
		stoneDivisor := float64(dismantleStoneDivisors[grade])
		attributeCount := min(2, int(math.RoundToEven(float64(alchemyResidue(r.roll, 6)+5)*float64(value)/100/stoneDivisor)))
		magicCount := min(2, int(math.RoundToEven(float64(alchemyResidue(r.roll, 6)+5)*float64(len(item.MagicOptions))*float64(value)/100/stoneDivisor)))
		for _, family := range []struct {
			count int
			types []int
		}{{attributeCount, []int{2}}, {magicCount, []int{1, 7}}} {
			for range family.count {
				outputRef, found := randomDismantleStone(r.roll, ref.Degree(), family.types)
				if !found {
					return result, ErrAlchemyUnsupported
				}
				outputs = append(outputs, plannedOutput{ref: outputRef, amount: 1})
			}
		}
		if meritRef, produced := dismantleMeritOutput(item, ref); produced {
			outputs = append(outputs, plannedOutput{ref: meritRef, amount: 1})
		}
	}
	if err = consumeAlchemyCode(ctx, tx, characterID, inventory, alchemyRondoEquip, requiredRondo, &result.Materials); err != nil {
		return result, err
	}
	for _, slot := range slots {
		item := inventory.Items[slot]
		if err = consumeAlchemyAmount(ctx, tx, characterID, inventory, slot, item, 1, &result.Materials); err != nil {
			return result, err
		}
	}
	for _, output := range outputs {
		if _, err = addAlchemyOutput(ctx, tx, characterID, inventory, output.ref, output.amount); err != nil {
			return result, err
		}
	}
	result.Success = true
	result.Inventory, err = loadInventoryInItemUseTransaction(ctx, tx, characterID)
	if err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return AlchemyResult{}, fmt.Errorf("%w: %v", ErrAlchemyOutcomeUnknown, err)
	}
	return result, nil
}

var dismantleGradeDivisors = [12]int{75, 140, 215, 290, 365, 458, 551, 660, 776, 903, 1035, 1180}

var dismantleStoneDivisors = [12]int{18750, 34950, 53700, 72340, 91150, 114280, 137690, 164830, 193830, 225240, 258180, 294706}

func dismantleMeritOutput(item Item, ref RefItem) (RefItem, bool) {
	code, ok := dismantleMeritCode(item, ref)
	if !ok {
		return RefItem{}, false
	}
	return alchemyRefByCode(code)
}

func dismantleMeritCode(item Item, ref RefItem) (string, bool) {
	category, index, ok := dismantleMeritCategory(ref)
	if !ok {
		return "", false
	}
	rarityLevel := ref.Rarity
	if ref.SetID != 0 {
		rarityLevel += 4
	}
	rarityMerit := map[int]float64{0: 1, 2: 8, 4: 3, 6: 8}[rarityLevel]
	if rarityMerit == 0 {
		return "", false
	}
	classMerit := float64((ref.ItemClass-1)%3 + 1)
	plusMerit := 2.0
	if item.OptLevel >= 7 {
		plusMerit = 6
	} else if item.OptLevel >= 5 {
		plusMerit = 4
	}
	score := rarityMerit*classMerit + plusMerit + dismantleVarianceMerit(item.Variance, category, index)
	meritGrade := 0
	switch rounded := int(math.RoundToEven(score)); {
	case rounded >= 20:
		meritGrade = 2
	case rounded >= 10:
		meritGrade = 1
	default:
		return "", false
	}
	gradeCode := "A"
	if meritGrade == 2 {
		gradeCode = "B"
	}
	rareCode := ""
	if ref.Rarity == 2 {
		rareCode = "RARE_"
	}
	code := fmt.Sprintf("ITEM_ETC_ARCHEMY_UPPER_REINFORCE_RECIPE_%s_%s%s_%d", category, rareCode, gradeCode, ref.Degree())
	return code, true
}

func dismantleMeritCategory(ref RefItem) (string, int, bool) {
	switch ref.TypeID3 {
	case 6:
		return "WE", 0, true
	case 4:
		return "SH", 3, true
	case 5, 12:
		return "AC", 2, true
	case 1, 2, 3, 9, 10, 11:
		return "AR", 1, true
	default:
		return "", 0, false
	}
}

func dismantleVarianceMerit(variance uint64, category string, categoryIndex int) float64 {
	type meritField struct {
		shift       int
		coefficient [4]float64
	}
	fields := map[string][]meritField{
		"WE": {
			{0, [4]float64{2}}, {5, [4]float64{3}}, {10, [4]float64{3}},
			{15, [4]float64{2}}, {20, [4]float64{1}}, {25, [4]float64{1}}, {30, [4]float64{1}},
		},
		"AR": {
			{0, [4]float64{0, 2}}, {5, [4]float64{0, 3}}, {10, [4]float64{0, 3}},
			{15, [4]float64{0, 1}}, {20, [4]float64{0, 1}}, {25, [4]float64{0, 1}},
		},
		"SH": {
			{0, [4]float64{0, 0, 0, 3}}, {5, [4]float64{0, 0, 0, 3}},
			{10, [4]float64{0, 0, 0, 2}}, {15, [4]float64{0, 0, 0, 1}},
			{20, [4]float64{0, 0, 0, 1}}, {25, [4]float64{0, 0, 0, 1}},
		},
		"AC": {{0, [4]float64{0, 0, 2}}, {5, [4]float64{0, 0, 2}}},
	}
	correction := [4]float64{0.6, 0.7, 0.6, 0.6}[categoryIndex]
	total := 0.0
	for _, field := range fields[category] {
		bucket := float64((variance >> field.shift) & VarianceStatMask)
		total += (bucket / float64(VarianceStatMask) / 100) * field.coefficient[categoryIndex] * correction
	}
	return total
}

func randomDismantleStone(roll func() float64, degree int, typeID4 []int) (RefItem, bool) {
	allowed := make(map[int]struct{}, len(typeID4))
	for _, value := range typeID4 {
		allowed[value] = struct{}{}
	}
	type weightedCandidate struct {
		ref    RefItem
		weight int
	}
	candidates := make([]weightedCandidate, 0)
	totalWeight := 0
	for _, ref := range RefItems {
		if ref.TypeID1 != 3 || ref.TypeID2 != 3 || ref.TypeID3 != 11 || ref.ItemClass != degree {
			continue
		}
		if _, ok := allowed[ref.TypeID4]; ok {
			weight := dismantleStoneWeight(ref)
			if weight > 0 {
				candidates = append(candidates, weightedCandidate{ref: ref, weight: weight})
				totalWeight += weight
			}
		}
	}
	if len(candidates) == 0 || totalWeight <= 0 {
		return RefItem{}, false
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ref.ID < candidates[j].ref.ID })
	selected := alchemyResidue(roll, totalWeight)
	for _, candidate := range candidates {
		if selected < candidate.weight {
			return candidate.ref, true
		}
		selected -= candidate.weight
	}
	return candidates[len(candidates)-1].ref, true
}

var dismantleMagicStoneWeights = map[string]int{
	"STR": 30, "INT": 60, "DUR": 30, "HR": 100, "EVADE_BLOCK": 30,
	"EVADE_CRITICAL": 30, "ER": 100, "HP": 30, "MP": 60,
	"FROSTBITE": 100, "ESHOCK": 100, "BURN": 100, "POISON": 100,
	"ZOMBIE": 100, "SOLID": 300, "LUCK": 300,
}

var dismantleAttributeStoneWeights = map[string]int{
	"PA": 60, "PASTR": 60, "MA": 60, "MAINT": 60, "PD": 60,
	"PDSTR": 60, "MD": 60, "MDINT": 60, "PAR": 60, "MAR": 60,
	"ER": 100, "HR": 100, "CRITICAL": 30, "BR": 30,
}

func dismantleStoneWeight(ref RefItem) int {
	code := strings.ToUpper(strings.TrimSpace(ref.CodeName))
	degreeSuffix := fmt.Sprintf("_%02d", ref.ItemClass)
	if !strings.HasSuffix(code, degreeSuffix) {
		return 0
	}
	code = strings.TrimSuffix(code, degreeSuffix)
	switch ref.TypeID4 {
	case 1, 7:
		const prefix = "ITEM_ETC_ARCHEMY_MAGICSTONE_"
		if !strings.HasPrefix(code, prefix) {
			return 0
		}
		return dismantleMagicStoneWeights[strings.TrimPrefix(code, prefix)]
	case 2:
		const prefix = "ITEM_ETC_ARCHEMY_ATTRSTONE_"
		if !strings.HasPrefix(code, prefix) {
			return 0
		}
		return dismantleAttributeStoneWeights[strings.TrimPrefix(code, prefix)]
	default:
		return 0
	}
}

func alchemyResidue(roll func() float64, modulus int) int {
	if modulus <= 1 || roll == nil {
		return 0
	}
	value := roll()
	if value < 0 || value >= 1 {
		return modulus - 1
	}
	return int(value * float64(modulus))
}

func (r *SQLAlchemyRepository) lockOwnedAlchemyInventory(ctx context.Context, characterID int64, userID uint32) (*sql.Tx, Inventory, error) {
	if r == nil || r.shard == nil || characterID <= 0 || userID == 0 {
		return nil, Inventory{}, ErrAlchemyInvalidRequest
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return nil, Inventory{}, err
	}
	var owner uint32
	if err = tx.QueryRowContext(ctx, "SELECT `FK_USER` FROM `CHAR` WHERE `id`=? FOR UPDATE", characterID).Scan(&owner); err != nil || owner != userID {
		_ = tx.Rollback()
		return nil, Inventory{}, ErrAlchemyConflict
	}
	inventory, err := loadLockedAlchemyInventory(ctx, tx, characterID)
	if err != nil {
		_ = tx.Rollback()
		return nil, Inventory{}, err
	}
	return tx, inventory, nil
}

func alchemyRecipeParts(ref RefItem) []alchemyRecipePart {
	values := []int{ref.Param1, ref.Param2, ref.Param3, ref.Param4}
	codes := []string{ref.Param1Desc, ref.Param2Desc, ref.Param3Desc, ref.Param4Desc}
	parts := make([]alchemyRecipePart, 0, 4)
	for index, code := range codes {
		code = strings.TrimSpace(code)
		if code == "" || strings.EqualFold(code, "xxx") || values[index] <= 0 {
			continue
		}
		parts = append(parts, alchemyRecipePart{code: code, amount: values[index]})
	}
	return parts
}

func alchemyRefByCode(code string) (RefItem, bool) {
	for _, ref := range RefItems {
		if strings.EqualFold(strings.TrimSpace(ref.CodeName), strings.TrimSpace(code)) {
			return ref, true
		}
	}
	return RefItem{}, false
}

func consumeAlchemyCode(ctx context.Context, tx *sql.Tx, characterID int64, inventory Inventory, code string, amount int, updates *[]AlchemyMaterialUpdate) error {
	allowed := make(map[byte]struct{}, len(inventory.Items))
	for slot := range inventory.Items {
		allowed[slot] = struct{}{}
	}
	return consumeAlchemyCodeFromSlots(ctx, tx, characterID, inventory, allowed, code, amount, updates)
}

func consumeAlchemyCodeFromSlots(ctx context.Context, tx *sql.Tx, characterID int64, inventory Inventory, allowed map[byte]struct{}, code string, amount int, updates *[]AlchemyMaterialUpdate) error {
	remaining := amount
	slots := make([]int, 0, len(allowed))
	for slot := range allowed {
		slots = append(slots, int(slot))
	}
	sort.Ints(slots)
	for _, value := range slots {
		if remaining == 0 {
			break
		}
		slot := byte(value)
		item, ok := inventory.Items[slot]
		if !ok {
			continue
		}
		ref, ok := RefItems[item.RefObjectID]
		if !ok || !strings.EqualFold(strings.TrimSpace(ref.CodeName), strings.TrimSpace(code)) {
			continue
		}
		consume := min(remaining, item.StackSize)
		if err := consumeAlchemyAmount(ctx, tx, characterID, inventory, slot, item, consume, updates); err != nil {
			return err
		}
		remaining -= consume
	}
	if remaining != 0 {
		return ErrAlchemyConflict
	}
	return nil
}

func consumeAlchemyAmount(ctx context.Context, tx *sql.Tx, characterID int64, inventory Inventory, slot byte, item Item, amount int, updates *[]AlchemyMaterialUpdate) error {
	if amount <= 0 || item.StackSize < amount {
		return ErrAlchemyConflict
	}
	remaining := item.StackSize - amount
	if remaining > 0 {
		result, err := tx.ExecContext(ctx, "UPDATE `ITEM` SET `STACK_COUNT`=?,`DATA`=? WHERE `id`=? AND `STACK_COUNT`=?", remaining, remaining, item.ID, item.StackSize)
		if err != nil {
			return err
		}
		if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
			return ErrAlchemyConflict
		}
	} else {
		result, err := tx.ExecContext(ctx, "DELETE FROM `INVENTORY` WHERE `FK_CHAR`=? AND `SLOT`=? AND `FK_ITEM`=?", characterID, slot, item.ID)
		if err != nil {
			return err
		}
		if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
			return ErrAlchemyConflict
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM `ITEM` WHERE `id`=?", item.ID); err != nil {
			return err
		}
		delete(inventory.Items, slot)
	}
	*updates = append(*updates, AlchemyMaterialUpdate{Slot: slot, Remaining: uint16(remaining)})
	if remaining > 0 {
		item.StackSize = remaining
		inventory.Items[slot] = item
	}
	return nil
}

func addAlchemyOutput(ctx context.Context, tx *sql.Tx, characterID int64, inventory Inventory, ref RefItem, amount int) (byte, error) {
	if amount <= 0 {
		return 0, ErrAlchemyConflict
	}
	maxStack := ref.StackSize
	if maxStack < 1 {
		maxStack = 1
	}
	firstSlot := byte(0)
	assigned := false
	for slot, item := range inventory.Items {
		if amount == 0 {
			break
		}
		if item.RefObjectID != ref.ID || maxStack <= 1 || item.StackSize >= maxStack {
			continue
		}
		added := min(amount, maxStack-item.StackSize)
		if _, err := tx.ExecContext(ctx, "UPDATE `ITEM` SET `STACK_COUNT`=`STACK_COUNT`+?,`DATA`=`DATA`+? WHERE `id`=?", added, added, item.ID); err != nil {
			return 0, err
		}
		item.StackSize += added
		item.Data += uint32(added)
		inventory.Items[slot] = item
		amount -= added
		if !assigned {
			firstSlot, assigned = slot, true
		}
	}
	for amount > 0 {
		var slot byte
		found := false
		for candidate := InventoryEquipmentSlots; candidate < BaseInventorySize; candidate++ {
			if _, occupied := inventory.Items[candidate]; !occupied {
				slot, found = candidate, true
				break
			}
		}
		if !found {
			return 0, ErrAlchemyConflict
		}
		stack := min(amount, maxStack)
		insert, err := tx.ExecContext(ctx, "INSERT INTO `ITEM` (`VARIANCE`,`FK_REF_ITEM`,`STACK_COUNT`,`OPT_LEVEL`,`DATA`) VALUES (0,?,?,0,?)", ref.ID, stack, stack)
		if err != nil {
			return 0, err
		}
		itemID, err := insert.LastInsertId()
		if err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO `INVENTORY` (`FK_CHAR`,`SLOT`,`FK_ITEM`) VALUES (?,?,?)", characterID, slot, itemID); err != nil {
			return 0, err
		}
		inventory.Items[slot] = Item{ID: int(itemID), StackSize: stack, Data: uint32(stack), SRObject: SRObject{RefObjectID: ref.ID}}
		amount -= stack
		if !assigned {
			firstSlot, assigned = slot, true
		}
	}
	return firstSlot, nil
}

func finishAlchemyMake(ctx context.Context, tx *sql.Tx, characterID int64, result AlchemyMakeResult) (AlchemyMakeResult, error) {
	var err error
	result.Inventory, err = loadInventoryInItemUseTransaction(ctx, tx, characterID)
	if err != nil {
		return AlchemyMakeResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return AlchemyMakeResult{}, fmt.Errorf("%w: %v", ErrAlchemyOutcomeUnknown, err)
	}
	return result, nil
}
