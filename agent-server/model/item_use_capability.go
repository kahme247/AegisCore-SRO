package model

import (
	"strings"
	"time"
)

// ItemUseFamily is a stable, audit-friendly name for an Agent item-use path.
// Values are strings so reports do not depend on enum ordinals when new
// families are added.
type ItemUseFamily string

const (
	ItemUseFamilyUnknown               ItemUseFamily = "unknown"
	ItemUseFamilyQuestInstanceEntry    ItemUseFamily = "quest_instance_entry"
	ItemUseFamilyCOSInstrument         ItemUseFamily = "cos_instrument"
	ItemUseFamilyCOSTransport          ItemUseFamily = "cos_transport"
	ItemUseFamilyCOSHealthRecovery     ItemUseFamily = "cos_health_recovery"
	ItemUseFamilyCOSHGPRecovery        ItemUseFamily = "cos_hgp_recovery"
	ItemUseFamilyCOSTimedService       ItemUseFamily = "cos_timed_service"
	ItemUseFamilyCOSRentalExtension    ItemUseFamily = "cos_rental_extension"
	ItemUseFamilyCOSRevival            ItemUseFamily = "cos_revival"
	ItemUseFamilyMonsterMask           ItemUseFamily = "monster_mask"
	ItemUseFamilyCollectionTalisman    ItemUseFamily = "collection_talisman"
	ItemUseFamilyDimensionHolePortal   ItemUseFamily = "dimension_hole_portal"
	ItemUseFamilyCharacterRecovery     ItemUseFamily = "character_recovery"
	ItemUseFamilyCharacterStatusCure   ItemUseFamily = "character_status_cure"
	ItemUseFamilyLinkedSkillBuff       ItemUseFamily = "linked_skill_buff"
	ItemUseFamilySpeedDrug             ItemUseFamily = "speed_drug"
	ItemUseFamilyLinkedSkillEffect     ItemUseFamily = "linked_skill_effect"
	ItemUseFamilyTimedCharacterService ItemUseFamily = "timed_character_service"
	ItemUseFamilyReturnScroll          ItemUseFamily = "return_scroll"
	ItemUseFamilyReverseReturnScroll   ItemUseFamily = "reverse_return_scroll"
	ItemUseFamilyCharacterResurrection ItemUseFamily = "character_resurrection"
	ItemUseFamilyDevilSpiritActivation ItemUseFamily = "devil_spirit_activation"
)

// ItemUseCapabilityStatus separates runtime coverage from reference-data
// quality. Conditional means a handler exists but the supplied classification
// context cannot prove an external or per-item reference. BrokenReference
// means the supplied data proves that the existing handler will reject it.
type ItemUseCapabilityStatus string

const (
	ItemUseCapabilitySupported       ItemUseCapabilityStatus = "supported"
	ItemUseCapabilityConditional     ItemUseCapabilityStatus = "conditional"
	ItemUseCapabilityBrokenReference ItemUseCapabilityStatus = "broken_reference"
	ItemUseCapabilityUnsupported     ItemUseCapabilityStatus = "unsupported"
)

// ItemUseCapabilityReason is a stable machine-readable explanation for the
// status. Human-facing audits can map these values to longer descriptions.
type ItemUseCapabilityReason string

const (
	ItemUseReasonNone                        ItemUseCapabilityReason = "none"
	ItemUseReasonItemNotUseable              ItemUseCapabilityReason = "item_not_useable"
	ItemUseReasonNoRuntimeHandler            ItemUseCapabilityReason = "no_runtime_handler"
	ItemUseReasonUnrecognizedCatalogItem     ItemUseCapabilityReason = "unrecognized_catalog_item"
	ItemUseReasonInvalidReferenceCode        ItemUseCapabilityReason = "invalid_reference_code"
	ItemUseReasonReferenceContextUnavailable ItemUseCapabilityReason = "reference_context_unavailable"
	ItemUseReasonReferenceNotFound           ItemUseCapabilityReason = "reference_not_found"
	ItemUseReasonReferenceKindInvalid        ItemUseCapabilityReason = "reference_kind_invalid"
	ItemUseReasonItemDataUnknown             ItemUseCapabilityReason = "item_data_unknown"
	ItemUseReasonItemDataMissing             ItemUseCapabilityReason = "item_data_missing"
	ItemUseReasonCollectionReferenceUnknown  ItemUseCapabilityReason = "collection_reference_unknown"
	ItemUseReasonCollectionReferenceMissing  ItemUseCapabilityReason = "collection_reference_missing"
	ItemUseReasonMissingLinkedSkill          ItemUseCapabilityReason = "missing_linked_skill"
	ItemUseReasonLinkedSkillCodeMismatch     ItemUseCapabilityReason = "linked_skill_code_mismatch"
	ItemUseReasonLinkedSkillKindUnsupported  ItemUseCapabilityReason = "linked_skill_kind_unsupported"
	ItemUseReasonInvalidBuffDuration         ItemUseCapabilityReason = "invalid_buff_duration"
	ItemUseReasonEmptyBuffModifiers          ItemUseCapabilityReason = "empty_buff_modifiers"
	ItemUseReasonInvalidSpeedDescriptor      ItemUseCapabilityReason = "invalid_speed_descriptor"
	ItemUseReasonInvalidEffectParameters     ItemUseCapabilityReason = "invalid_effect_parameters"
	ItemUseReasonInvalidServiceDescriptor    ItemUseCapabilityReason = "invalid_service_descriptor"
	ItemUseReasonServiceSkillUnknown         ItemUseCapabilityReason = "service_skill_unknown"
	ItemUseReasonServiceSkillMissing         ItemUseCapabilityReason = "service_skill_missing"
)

// ItemUsePrerequisite describes state checked by the repository after static
// classification. It is intentionally descriptive rather than executable;
// runtime callers retain authority over transactional validation.
type ItemUsePrerequisite string

const (
	ItemUsePrerequisiteQuestState                 ItemUsePrerequisite = "quest_state"
	ItemUsePrerequisiteCharacterReference         ItemUsePrerequisite = "character_reference"
	ItemUsePrerequisiteCharacterLevel             ItemUsePrerequisite = "character_level"
	ItemUsePrerequisiteOwnedCOSTarget             ItemUsePrerequisite = "owned_cos_target"
	ItemUsePrerequisiteLivingCOSTarget            ItemUsePrerequisite = "living_cos_target"
	ItemUsePrerequisiteDeadCOSTarget              ItemUsePrerequisite = "dead_cos_target"
	ItemUsePrerequisiteGrowthCOSTarget            ItemUsePrerequisite = "growth_cos_target"
	ItemUsePrerequisiteAbilityCOSTarget           ItemUsePrerequisite = "ability_cos_target"
	ItemUsePrerequisiteMissingCOSResource         ItemUsePrerequisite = "missing_cos_resource"
	ItemUsePrerequisiteServiceSkillReference      ItemUsePrerequisite = "service_skill_reference"
	ItemUsePrerequisiteItemData                   ItemUsePrerequisite = "item_data"
	ItemUsePrerequisiteCollectionReference        ItemUsePrerequisite = "collection_reference"
	ItemUsePrerequisiteUnregisteredCollectionSlot ItemUsePrerequisite = "unregistered_collection_slot"
	ItemUsePrerequisiteCharacterNeedsRecovery     ItemUsePrerequisite = "character_needs_recovery"
	ItemUsePrerequisiteLinkedSkill                ItemUsePrerequisite = "linked_skill"
	ItemUsePrerequisiteExtendableSpeedEffect      ItemUsePrerequisite = "extendable_speed_effect"
	ItemUsePrerequisiteCharacterServiceState      ItemUsePrerequisite = "character_service_state"
	ItemUsePrerequisiteReturnBind                 ItemUsePrerequisite = "return_bind"
	ItemUsePrerequisiteReverseReturnOption        ItemUsePrerequisite = "reverse_return_option"
	ItemUsePrerequisiteReverseReturnDestination   ItemUsePrerequisite = "reverse_return_destination"
	ItemUsePrerequisiteDeadCharacter              ItemUsePrerequisite = "dead_character"
	ItemUsePrerequisiteResurrectionRequest        ItemUsePrerequisite = "resurrection_request"
	ItemUsePrerequisiteDevilSpiritDuration        ItemUsePrerequisite = "devil_spirit_duration"
)

// ItemUseCapabilityInput contains catalog and optional instance/reference
// context. A nil CharacterReferences or SkillReferences map means that catalog
// was not supplied; a non-nil empty map means it was supplied and the reference
// is absent. ItemData follows the same convention: nil is unknown, while a
// pointer to zero is a known missing instance link.
type ItemUseCapabilityInput struct {
	RefItem                   RefItem
	LinkedSkill               *RefSkill
	ItemData                  *uint32
	CharacterReferences       map[uint32]RefChar
	SkillReferences           map[uint32]RefSkill
	CollectionReferenceExists *bool
}

type ItemUseCapability struct {
	Family        ItemUseFamily
	Status        ItemUseCapabilityStatus
	Reason        ItemUseCapabilityReason
	Prerequisites []ItemUsePrerequisite
}

// Implemented reports whether Agent owns a path for the family, even when an
// instance/reference prerequisite is currently missing.
func (c ItemUseCapability) Implemented() bool {
	return c.Status != ItemUseCapabilityUnsupported
}

// Ready reports whether all static and supplied reference checks passed.
// Transactional prerequisites may still reject a particular use attempt.
func (c ItemUseCapability) Ready() bool {
	return c.Status == ItemUseCapabilitySupported
}

func (c ItemUseCapability) HasPrerequisite(prerequisite ItemUsePrerequisite) bool {
	for _, candidate := range c.Prerequisites {
		if candidate == prerequisite {
			return true
		}
	}
	return false
}

// ClassifyItemUseCapability mirrors the item-use handler and
// SQLItemUseRepository dispatch order, including Devil Spirit activation and
// the inventory-selected UseResurrectionItem path. It performs no database or
// global-catalog reads.
func ClassifyItemUseCapability(input ItemUseCapabilityInput) ItemUseCapability {
	ref := input.RefItem
	if ref.IsDevilSpirit() {
		prerequisites := []ItemUsePrerequisite{ItemUsePrerequisiteDevilSpiritDuration}
		if input.ItemData == nil && ref.Param1 <= 0 {
			return itemUseCapability(ItemUseFamilyDevilSpiritActivation, ItemUseCapabilityConditional, ItemUseReasonItemDataUnknown, prerequisites...)
		}
		if input.ItemData != nil && *input.ItemData == 0 && ref.Param1 <= 0 {
			return itemUseCapability(ItemUseFamilyDevilSpiritActivation, ItemUseCapabilityBrokenReference, ItemUseReasonInvalidEffectParameters, prerequisites...)
		}
		return itemUseCapability(ItemUseFamilyDevilSpiritActivation, ItemUseCapabilitySupported, ItemUseReasonNone, prerequisites...)
	}
	if !ref.IsUseable {
		return itemUseCapability(inferItemUseFamily(ref), ItemUseCapabilityUnsupported, ItemUseReasonItemNotUseable)
	}

	if _, ok := QuestInstanceEntryQuestID(ref.CodeName); ok {
		return itemUseCapability(ItemUseFamilyQuestInstanceEntry, ItemUseCapabilitySupported, ItemUseReasonNone,
			ItemUsePrerequisiteQuestState)
	}

	if itemUseType(ref, 3, 3, 12) && ref.TypeID4 == 7 {
		if _, ok := DimensionHoleDefinitionForReference(ref); !ok {
			return itemUseCapability(ItemUseFamilyDimensionHolePortal, ItemUseCapabilityUnsupported, ItemUseReasonUnrecognizedCatalogItem)
		}
		// The exact stock item is recognized, but it must remain unsupported
		// until portal publication and instance admission are wired together.
		return itemUseCapability(ItemUseFamilyDimensionHolePortal, ItemUseCapabilityUnsupported, ItemUseReasonNoRuntimeHandler)
	}

	if itemUseType(ref, 3, 2, 1) && (ref.TypeID4 == 1 || ref.TypeID4 == 2) {
		return classifyCOSCharacterReference(input, ItemUseFamilyCOSInstrument, false)
	}

	if itemUseType(ref, 3, 3, 3) && ref.TypeID4 == 2 {
		return classifyCOSCharacterReference(input, ItemUseFamilyCOSTransport, true)
	}

	if itemUseType(ref, 3, 3, 1) && ref.TypeID4 == 4 {
		if ref.Param1 <= 0 {
			return itemUseCapability(ItemUseFamilyCOSHealthRecovery, ItemUseCapabilityBrokenReference, ItemUseReasonInvalidEffectParameters,
				ItemUsePrerequisiteOwnedCOSTarget, ItemUsePrerequisiteLivingCOSTarget, ItemUsePrerequisiteMissingCOSResource)
		}
		return itemUseCapability(ItemUseFamilyCOSHealthRecovery, ItemUseCapabilitySupported, ItemUseReasonNone,
			ItemUsePrerequisiteOwnedCOSTarget, ItemUsePrerequisiteLivingCOSTarget, ItemUsePrerequisiteMissingCOSResource)
	}

	if itemUseType(ref, 3, 3, 1) && ref.TypeID4 == 9 {
		if ref.Param1 <= 0 {
			return itemUseCapability(ItemUseFamilyCOSHGPRecovery, ItemUseCapabilityBrokenReference, ItemUseReasonInvalidEffectParameters,
				ItemUsePrerequisiteOwnedCOSTarget, ItemUsePrerequisiteLivingCOSTarget, ItemUsePrerequisiteGrowthCOSTarget, ItemUsePrerequisiteMissingCOSResource)
		}
		return itemUseCapability(ItemUseFamilyCOSHGPRecovery, ItemUseCapabilitySupported, ItemUseReasonNone,
			ItemUsePrerequisiteOwnedCOSTarget, ItemUsePrerequisiteLivingCOSTarget, ItemUsePrerequisiteGrowthCOSTarget, ItemUsePrerequisiteMissingCOSResource)
	}

	if itemUseType(ref, 3, 3, 13) && ref.TypeID4 == 15 {
		return classifyCOSTimedService(input)
	}

	if itemUseType(ref, 3, 3, 13) && ref.TypeID4 == 12 {
		if ref.Param1 <= 0 {
			return itemUseCapability(ItemUseFamilyCOSRentalExtension, ItemUseCapabilityBrokenReference, ItemUseReasonInvalidEffectParameters,
				ItemUsePrerequisiteOwnedCOSTarget, ItemUsePrerequisiteAbilityCOSTarget)
		}
		return itemUseCapability(ItemUseFamilyCOSRentalExtension, ItemUseCapabilitySupported, ItemUseReasonNone,
			ItemUsePrerequisiteOwnedCOSTarget, ItemUsePrerequisiteAbilityCOSTarget)
	}

	if IsCOSRevival(ref) {
		return itemUseCapability(ItemUseFamilyCOSRevival, ItemUseCapabilitySupported, ItemUseReasonNone,
			ItemUsePrerequisiteOwnedCOSTarget, ItemUsePrerequisiteDeadCOSTarget)
	}

	if itemUseType(ref, 3, 2, 2) {
		return classifyMonsterMask(input)
	}

	if itemUseType(ref, 3, 3, 12) && ref.TypeID4 == 5 {
		prerequisites := []ItemUsePrerequisite{ItemUsePrerequisiteCollectionReference, ItemUsePrerequisiteUnregisteredCollectionSlot}
		if input.CollectionReferenceExists == nil {
			return itemUseCapability(ItemUseFamilyCollectionTalisman, ItemUseCapabilityConditional, ItemUseReasonCollectionReferenceUnknown, prerequisites...)
		}
		if !*input.CollectionReferenceExists {
			return itemUseCapability(ItemUseFamilyCollectionTalisman, ItemUseCapabilityBrokenReference, ItemUseReasonCollectionReferenceMissing, prerequisites...)
		}
		return itemUseCapability(ItemUseFamilyCollectionTalisman, ItemUseCapabilitySupported, ItemUseReasonNone, prerequisites...)
	}

	if itemUseType(ref, 3, 3, 1) && ref.TypeID4 >= 1 && ref.TypeID4 <= 3 {
		if ref.Param1 <= 0 && ref.Param2 <= 0 && ref.Param3 <= 0 && ref.Param4 <= 0 {
			return itemUseCapability(ItemUseFamilyCharacterRecovery, ItemUseCapabilityBrokenReference, ItemUseReasonInvalidEffectParameters,
				ItemUsePrerequisiteCharacterNeedsRecovery)
		}
		return itemUseCapability(ItemUseFamilyCharacterRecovery, ItemUseCapabilitySupported, ItemUseReasonNone,
			ItemUsePrerequisiteCharacterNeedsRecovery)
	}

	if itemUseType(ref, 3, 3, 2) && (ref.TypeID4 == 1 || ref.TypeID4 == 6) {
		if _, ok := ItemCureFromReference(ref); !ok {
			return itemUseCapability(ItemUseFamilyCharacterStatusCure, ItemUseCapabilityBrokenReference, ItemUseReasonInvalidEffectParameters)
		}
		return itemUseCapability(ItemUseFamilyCharacterStatusCure, ItemUseCapabilitySupported, ItemUseReasonNone)
	}

	if itemUseType(ref, 3, 3, 13) && ref.TypeID4 == 1 {
		return classifyLinkedItemEffect(input)
	}

	if isItemServiceCandidate(ref) {
		if _, err := itemServiceFromReference(ref, time.Time{}); err != nil {
			return itemUseCapability(ItemUseFamilyTimedCharacterService, ItemUseCapabilityBrokenReference, ItemUseReasonInvalidServiceDescriptor,
				ItemUsePrerequisiteCharacterServiceState)
		}
		return itemUseCapability(ItemUseFamilyTimedCharacterService, ItemUseCapabilitySupported, ItemUseReasonNone,
			ItemUsePrerequisiteCharacterServiceState)
	}

	if itemUseType(ref, 3, 3, 3) && ref.TypeID4 == 1 {
		if ref.Param1 <= 0 || !strings.EqualFold(strings.TrimSpace(ref.Param3Desc), "RESURRECT") {
			return itemUseCapability(ItemUseFamilyReturnScroll, ItemUseCapabilityBrokenReference, ItemUseReasonInvalidEffectParameters,
				ItemUsePrerequisiteReturnBind)
		}
		return itemUseCapability(ItemUseFamilyReturnScroll, ItemUseCapabilitySupported, ItemUseReasonNone,
			ItemUsePrerequisiteReturnBind)
	}

	if itemUseType(ref, 3, 3, 3) && ref.TypeID4 == 3 {
		if ref.Param1 <= 0 {
			return itemUseCapability(ItemUseFamilyReverseReturnScroll, ItemUseCapabilityBrokenReference, ItemUseReasonInvalidEffectParameters,
				ItemUsePrerequisiteReverseReturnOption, ItemUsePrerequisiteReverseReturnDestination)
		}
		return itemUseCapability(ItemUseFamilyReverseReturnScroll, ItemUseCapabilitySupported, ItemUseReasonNone,
			ItemUsePrerequisiteReverseReturnOption, ItemUsePrerequisiteReverseReturnDestination)
	}

	if itemUseType(ref, 3, 3, 13) && ref.TypeID4 == 6 {
		return itemUseCapability(ItemUseFamilyCharacterResurrection, ItemUseCapabilitySupported, ItemUseReasonNone,
			ItemUsePrerequisiteDeadCharacter, ItemUsePrerequisiteResurrectionRequest)
	}

	return itemUseCapability(ItemUseFamilyUnknown, ItemUseCapabilityUnsupported, ItemUseReasonNoRuntimeHandler)
}

func itemUseCapability(family ItemUseFamily, status ItemUseCapabilityStatus, reason ItemUseCapabilityReason, prerequisites ...ItemUsePrerequisite) ItemUseCapability {
	return ItemUseCapability{Family: family, Status: status, Reason: reason, Prerequisites: prerequisites}
}

func itemUseType(ref RefItem, typeID1, typeID2, typeID3 int) bool {
	return ref.TypeID1 == typeID1 && ref.TypeID2 == typeID2 && ref.TypeID3 == typeID3
}

func inferItemUseFamily(ref RefItem) ItemUseFamily {
	if ref.IsDevilSpirit() {
		return ItemUseFamilyDevilSpiritActivation
	}
	if _, ok := QuestInstanceEntryQuestID(ref.CodeName); ok {
		return ItemUseFamilyQuestInstanceEntry
	}
	switch {
	case itemUseType(ref, 3, 2, 1) && (ref.TypeID4 == 1 || ref.TypeID4 == 2):
		return ItemUseFamilyCOSInstrument
	case itemUseType(ref, 3, 2, 2):
		return ItemUseFamilyMonsterMask
	case itemUseType(ref, 3, 3, 1) && ref.TypeID4 >= 1 && ref.TypeID4 <= 3:
		return ItemUseFamilyCharacterRecovery
	case itemUseType(ref, 3, 3, 2) && (ref.TypeID4 == 1 || ref.TypeID4 == 6):
		return ItemUseFamilyCharacterStatusCure
	case itemUseType(ref, 3, 3, 1) && ref.TypeID4 == 4:
		return ItemUseFamilyCOSHealthRecovery
	case IsCOSRevival(RefItem{RefObject: RefObject{IsUseable: true, CodeName: ref.CodeName, TypeInfo: ref.TypeInfo}}):
		return ItemUseFamilyCOSRevival
	case itemUseType(ref, 3, 3, 1) && ref.TypeID4 == 9:
		return ItemUseFamilyCOSHGPRecovery
	case itemUseType(ref, 3, 3, 3) && ref.TypeID4 == 1:
		return ItemUseFamilyReturnScroll
	case itemUseType(ref, 3, 3, 3) && ref.TypeID4 == 2:
		return ItemUseFamilyCOSTransport
	case itemUseType(ref, 3, 3, 3) && ref.TypeID4 == 3:
		return ItemUseFamilyReverseReturnScroll
	case itemUseType(ref, 3, 3, 12) && ref.TypeID4 == 5:
		return ItemUseFamilyCollectionTalisman
	case itemUseType(ref, 3, 3, 12) && ref.TypeID4 == 7:
		return ItemUseFamilyDimensionHolePortal
	case itemUseType(ref, 3, 3, 13) && ref.TypeID4 == 1:
		return ItemUseFamilyLinkedSkillEffect
	case isItemServiceCandidateIgnoringUsable(ref):
		return ItemUseFamilyTimedCharacterService
	case itemUseType(ref, 3, 3, 13) && ref.TypeID4 == 6:
		return ItemUseFamilyCharacterResurrection
	case itemUseType(ref, 3, 3, 13) && ref.TypeID4 == 12:
		return ItemUseFamilyCOSRentalExtension
	case itemUseType(ref, 3, 3, 13) && ref.TypeID4 == 15:
		return ItemUseFamilyCOSTimedService
	default:
		return ItemUseFamilyUnknown
	}
}

func classifyCOSCharacterReference(input ItemUseCapabilityInput, family ItemUseFamily, transport bool) ItemUseCapability {
	ref := input.RefItem
	prerequisites := []ItemUsePrerequisite{ItemUsePrerequisiteCharacterReference}
	if transport {
		prerequisites = append(prerequisites, ItemUsePrerequisiteCharacterLevel)
		name := strings.ToUpper(ref.CodeName)
		if !strings.Contains(name, "COS_T_") && !strings.Contains(name, "COS_C_") {
			return itemUseCapability(family, ItemUseCapabilityBrokenReference, ItemUseReasonInvalidReferenceCode, prerequisites...)
		}
	} else if !strings.Contains(strings.ToUpper(ref.CodeName), "COS_") {
		return itemUseCapability(family, ItemUseCapabilityBrokenReference, ItemUseReasonInvalidReferenceCode, prerequisites...)
	}

	code := strings.TrimSpace(ref.Param1Desc)
	if code == "" || strings.EqualFold(code, "xxx") {
		return itemUseCapability(family, ItemUseCapabilityBrokenReference, ItemUseReasonInvalidReferenceCode, prerequisites...)
	}
	if input.CharacterReferences == nil {
		return itemUseCapability(family, ItemUseCapabilityConditional, ItemUseReasonReferenceContextUnavailable, prerequisites...)
	}
	found, valid := findItemUseCharacterReference(input.CharacterReferences, code, transport)
	if !found {
		return itemUseCapability(family, ItemUseCapabilityBrokenReference, ItemUseReasonReferenceNotFound, prerequisites...)
	}
	if !valid {
		return itemUseCapability(family, ItemUseCapabilityBrokenReference, ItemUseReasonReferenceKindInvalid, prerequisites...)
	}
	return itemUseCapability(family, ItemUseCapabilitySupported, ItemUseReasonNone, prerequisites...)
}

func findItemUseCharacterReference(references map[uint32]RefChar, code string, transport bool) (found, valid bool) {
	for _, ref := range references {
		if strings.EqualFold(strings.TrimSpace(ref.CodeName), code) {
			return true, validItemUseCharacterReference(ref, transport)
		}
	}
	if !transport {
		return false, false
	}
	prefix := strings.ToUpper(code) + "_"
	for _, ref := range references {
		if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(ref.CodeName)), prefix) {
			continue
		}
		found = true
		if validItemUseCharacterReference(ref, true) {
			valid = true
		}
	}
	return found, valid
}

func validItemUseCharacterReference(ref RefChar, transport bool) bool {
	if ref.ID == 0 || ref.TypeID1 != 1 || ref.TypeID2 != 2 || ref.TypeID3 != 3 {
		return false
	}
	if transport {
		return ref.TypeID4 == 1 || ref.TypeID4 == 2
	}
	return COSKind(ref.TypeID4).Valid()
}

func classifyMonsterMask(input ItemUseCapabilityInput) ItemUseCapability {
	prerequisites := []ItemUsePrerequisite{ItemUsePrerequisiteItemData, ItemUsePrerequisiteCharacterReference}
	if input.ItemData == nil {
		return itemUseCapability(ItemUseFamilyMonsterMask, ItemUseCapabilityConditional, ItemUseReasonItemDataUnknown, prerequisites...)
	}
	if *input.ItemData == 0 {
		return itemUseCapability(ItemUseFamilyMonsterMask, ItemUseCapabilityBrokenReference, ItemUseReasonItemDataMissing, prerequisites...)
	}
	if input.CharacterReferences == nil {
		return itemUseCapability(ItemUseFamilyMonsterMask, ItemUseCapabilityConditional, ItemUseReasonReferenceContextUnavailable, prerequisites...)
	}
	if _, ok := input.CharacterReferences[*input.ItemData]; !ok {
		return itemUseCapability(ItemUseFamilyMonsterMask, ItemUseCapabilityBrokenReference, ItemUseReasonReferenceNotFound, prerequisites...)
	}
	return itemUseCapability(ItemUseFamilyMonsterMask, ItemUseCapabilitySupported, ItemUseReasonNone, prerequisites...)
}

const itemUseSpeedPercentMarker = 1752396901

func classifyLinkedItemEffect(input ItemUseCapabilityInput) ItemUseCapability {
	prerequisites := []ItemUsePrerequisite{ItemUsePrerequisiteLinkedSkill}
	code := strings.TrimSpace(input.RefItem.Param1Desc)
	if code == "" || strings.EqualFold(code, "xxx") || input.LinkedSkill == nil || input.LinkedSkill.ID == 0 {
		return itemUseCapability(ItemUseFamilyLinkedSkillEffect, ItemUseCapabilityBrokenReference, ItemUseReasonMissingLinkedSkill, prerequisites...)
	}
	skill := *input.LinkedSkill
	if skill.CodeName != "" && !strings.EqualFold(strings.TrimSpace(skill.CodeName), code) {
		return itemUseCapability(ItemUseFamilyLinkedSkillEffect, ItemUseCapabilityBrokenReference, ItemUseReasonLinkedSkillCodeMismatch, prerequisites...)
	}
	if skill.SupportsItemUseBuff() {
		return itemUseCapability(ItemUseFamilyLinkedSkillBuff, ItemUseCapabilitySupported, ItemUseReasonNone, prerequisites...)
	}
	if validItemUseSpeedDescriptor(skill) {
		return itemUseCapability(ItemUseFamilySpeedDrug, ItemUseCapabilitySupported, ItemUseReasonNone,
			ItemUsePrerequisiteLinkedSkill, ItemUsePrerequisiteExtendableSpeedEffect)
	}
	if skill.ExecutionKind() == SkillExecutionBuff {
		if skill.BuffDuration <= 0 {
			return itemUseCapability(ItemUseFamilyLinkedSkillBuff, ItemUseCapabilityBrokenReference, ItemUseReasonInvalidBuffDuration, prerequisites...)
		}
		return itemUseCapability(ItemUseFamilyLinkedSkillBuff, ItemUseCapabilityBrokenReference, ItemUseReasonEmptyBuffModifiers, prerequisites...)
	}
	if skill.ExecutionKind() == SkillExecutionUnsupported {
		return itemUseCapability(ItemUseFamilySpeedDrug, ItemUseCapabilityBrokenReference, ItemUseReasonInvalidSpeedDescriptor,
			ItemUsePrerequisiteLinkedSkill, ItemUsePrerequisiteExtendableSpeedEffect)
	}
	return itemUseCapability(ItemUseFamilyLinkedSkillEffect, ItemUseCapabilityUnsupported, ItemUseReasonLinkedSkillKindUnsupported, prerequisites...)
}

func validItemUseSpeedDescriptor(skill RefSkill) bool {
	return skill.Params[2] == skillParameterDuration && skill.Params[3] > 0 &&
		skill.Params[4] == itemUseSpeedPercentMarker && skill.Params[5] > 0
}

func classifyCOSTimedService(input ItemUseCapabilityInput) ItemUseCapability {
	ref := input.RefItem
	code, target := cosTimedServiceCapabilityReference(ref.ID)
	if code == "" {
		return itemUseCapability(ItemUseFamilyCOSTimedService, ItemUseCapabilityUnsupported, ItemUseReasonUnrecognizedCatalogItem,
			ItemUsePrerequisiteOwnedCOSTarget, ItemUsePrerequisiteServiceSkillReference)
	}
	prerequisites := []ItemUsePrerequisite{ItemUsePrerequisiteOwnedCOSTarget, target, ItemUsePrerequisiteServiceSkillReference}
	if ref.Param1 <= 0 {
		return itemUseCapability(ItemUseFamilyCOSTimedService, ItemUseCapabilityBrokenReference, ItemUseReasonInvalidEffectParameters, prerequisites...)
	}
	if input.SkillReferences == nil {
		return itemUseCapability(ItemUseFamilyCOSTimedService, ItemUseCapabilityConditional, ItemUseReasonServiceSkillUnknown, prerequisites...)
	}
	for id, skill := range input.SkillReferences {
		if id != 0 && skill.CodeName == code {
			return itemUseCapability(ItemUseFamilyCOSTimedService, ItemUseCapabilitySupported, ItemUseReasonNone, prerequisites...)
		}
	}
	return itemUseCapability(ItemUseFamilyCOSTimedService, ItemUseCapabilityBrokenReference, ItemUseReasonServiceSkillMissing, prerequisites...)
}

func cosTimedServiceCapabilityReference(refItemID uint32) (string, ItemUsePrerequisite) {
	switch refItemID {
	case 22712, 35874:
		return "SKILL_MALL_PET_GROWTH_POTION_01", ItemUsePrerequisiteGrowthCOSTarget
	case 22713:
		return "SKILL_MALL_PET_SKILL_COLD", ItemUsePrerequisiteGrowthCOSTarget
	case 22714:
		return "SKILL_MALL_PET_SKILL_FIRE", ItemUsePrerequisiteGrowthCOSTarget
	case 22715:
		return "SKILL_MALL_PET_SKILL_LIGHTNING", ItemUsePrerequisiteGrowthCOSTarget
	case 22710:
		return "SKILL_MALL_PET_WATCH_CHULHYEN_01", ItemUsePrerequisiteAbilityCOSTarget
	case 22711:
		return "SKILL_MALL_PET_WATCH_AGOL_01", ItemUsePrerequisiteAbilityCOSTarget
	case 22926:
		return "SKILL_MALL_PET_WATCH_CHULHYEN_02", ItemUsePrerequisiteAbilityCOSTarget
	case 22927:
		return "SKILL_MALL_PET_WATCH_AGOL_02", ItemUsePrerequisiteAbilityCOSTarget
	default:
		return "", ItemUsePrerequisiteOwnedCOSTarget
	}
}

func isItemServiceCandidate(ref RefItem) bool {
	return ref.IsUseable && isItemServiceCandidateIgnoringUsable(ref)
}

func isItemServiceCandidateIgnoringUsable(ref RefItem) bool {
	if ref.TypeID1 != 3 || ref.TypeID2 != 3 {
		return false
	}
	if ref.TypeID3 == 13 {
		return ref.TypeID4 == 4 || ref.TypeID4 == 5 || ref.TypeID4 == 14
	}
	return ref.TypeID3 == 3 && ref.TypeID4 == 10
}
