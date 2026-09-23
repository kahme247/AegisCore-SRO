package model

import (
	"reflect"
	"testing"
)

func TestClassifyItemUseCapability(t *testing.T) {
	validCOS := RefChar{RefObject: RefObject{ID: 700, CodeName: "COS_P_WOLF", TypeInfo: TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 3, TypeID4: 3}}}
	validTransport := RefChar{RefObject: RefObject{ID: 701, CodeName: "COS_T_ELEPHANT_01", TypeInfo: TypeInfo{TypeID1: 1, TypeID2: 2, TypeID3: 3, TypeID4: 2}}}
	invalidCOS := RefChar{RefObject: RefObject{ID: 702, CodeName: "COS_P_WOLF", TypeInfo: TypeInfo{TypeID1: 1, TypeID2: 1, TypeID3: 1, TypeID4: 1}}}

	buffSkill := RefSkill{
		ID: 11, CodeName: "SKILL_TEST_ITEM_BUFF", BasicActivity: 2, BuffDuration: 60_000,
		BuffModifiers: BuffModifiers{HPPercent: 10},
	}
	speedSkill := RefSkill{ID: 12, CodeName: "SKILL_TEST_SPEED", BasicActivity: 2}
	speedSkill.Params[2] = skillParameterDuration
	speedSkill.Params[3] = 600_000
	speedSkill.Params[4] = itemUseSpeedPercentMarker
	speedSkill.Params[5] = 50
	healingSkill := RefSkill{ID: 13, CodeName: "SKILL_TEST_HEAL", BasicActivity: 2, HealHP: 100}
	mallSystemBuff := RefSkill{ID: 17, CodeName: "SKILL_MALL_HP_INC_430_POTION_01", BasicActivity: 2}
	copy(mallSystemBuff.Params[:], []int{3, skillParameterSystemBuff, skillParameterDuration, 1_800_000, skillParameterHP, 430, 0})
	mallSystemBuff.parseParameters()
	unresolvedMallEffect := RefSkill{ID: 18, CodeName: "SKILL_MALL_DAMAGE_ABS_20P_SCROLL_01", BasicActivity: 2}
	copy(unresolvedMallEffect.Params[:], []int{3, skillParameterSystemBuff, skillParameterDuration, 1_800_000, 1868849522, 15, 20})
	unresolvedMallEffect.parseParameters()
	invalidDurationBuff := RefSkill{ID: 14, CodeName: "SKILL_TEST_BAD_DURATION", BasicActivity: 2, UpkeepIntervalMS: 1_000, BuffModifiers: BuffModifiers{Strength: 5}}
	emptyBuff := RefSkill{ID: 15, CodeName: "SKILL_TEST_EMPTY_BUFF", BasicActivity: 2, BuffDuration: 60_000}
	invalidSpeed := RefSkill{ID: 16, CodeName: "SKILL_TEST_BAD_SPEED", BasicActivity: 2}

	tests := []struct {
		name          string
		input         ItemUseCapabilityInput
		family        ItemUseFamily
		status        ItemUseCapabilityStatus
		reason        ItemUseCapabilityReason
		prerequisites []ItemUsePrerequisite
	}{
		{
			name:   "non-useable known family",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestItem(false, 3, 1, 1)},
			family: ItemUseFamilyCharacterRecovery, status: ItemUseCapabilityUnsupported, reason: ItemUseReasonItemNotUseable,
		},
		{
			name:   "quest instance entry",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestItemWithCode(3, 12, 1, "ITEM_QNO_EU_IVY_3_06")},
			family: ItemUseFamilyQuestInstanceEntry, status: ItemUseCapabilitySupported, reason: ItemUseReasonNone,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteQuestState},
		},
		{
			name:   "recognized Dimension Hole remains blocked until runtime wiring",
			input:  ItemUseCapabilityInput{RefItem: dimensionHoleTestReference(DimensionHoleCatalog()[0])},
			family: ItemUseFamilyDimensionHolePortal, status: ItemUseCapabilityUnsupported, reason: ItemUseReasonNoRuntimeHandler,
		},
		{
			name: "unrecognized Dimension Hole catalog item",
			input: ItemUseCapabilityInput{RefItem: RefItem{RefObject: RefObject{
				ID: 99999, CodeName: "ITEM_ETC_TELEPORT_HOLE_UNKNOWN", IsUseable: true,
				TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 12, TypeID4: 7},
			}}},
			family: ItemUseFamilyDimensionHolePortal, status: ItemUseCapabilityUnsupported, reason: ItemUseReasonUnrecognizedCatalogItem,
		},
		{
			name:   "COS instrument reference context unknown",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestCOSInstrument()},
			family: ItemUseFamilyCOSInstrument, status: ItemUseCapabilityConditional, reason: ItemUseReasonReferenceContextUnavailable,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteCharacterReference},
		},
		{
			name:   "COS instrument supported",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestCOSInstrument(), CharacterReferences: map[uint32]RefChar{validCOS.ID: validCOS}},
			family: ItemUseFamilyCOSInstrument, status: ItemUseCapabilitySupported, reason: ItemUseReasonNone,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteCharacterReference},
		},
		{
			name:   "COS instrument invalid item code",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestItem(true, 2, 1, 1)},
			family: ItemUseFamilyCOSInstrument, status: ItemUseCapabilityBrokenReference, reason: ItemUseReasonInvalidReferenceCode,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteCharacterReference},
		},
		{
			name:   "COS instrument linked reference missing",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestCOSInstrument(), CharacterReferences: map[uint32]RefChar{}},
			family: ItemUseFamilyCOSInstrument, status: ItemUseCapabilityBrokenReference, reason: ItemUseReasonReferenceNotFound,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteCharacterReference},
		},
		{
			name:   "COS instrument linked reference has wrong kind",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestCOSInstrument(), CharacterReferences: map[uint32]RefChar{invalidCOS.ID: invalidCOS}},
			family: ItemUseFamilyCOSInstrument, status: ItemUseCapabilityBrokenReference, reason: ItemUseReasonReferenceKindInvalid,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteCharacterReference},
		},
		{
			name:   "transport resolves level-suffixed reference",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestTransport(), CharacterReferences: map[uint32]RefChar{validTransport.ID: validTransport}},
			family: ItemUseFamilyCOSTransport, status: ItemUseCapabilitySupported, reason: ItemUseReasonNone,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteCharacterReference, ItemUsePrerequisiteCharacterLevel},
		},
		{
			name:   "transport item code is not a COS family",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestItem(true, 3, 3, 2)},
			family: ItemUseFamilyCOSTransport, status: ItemUseCapabilityBrokenReference, reason: ItemUseReasonInvalidReferenceCode,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteCharacterReference, ItemUsePrerequisiteCharacterLevel},
		},
		{
			name:   "COS health potion",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestPositiveItem(3, 1, 4)},
			family: ItemUseFamilyCOSHealthRecovery, status: ItemUseCapabilitySupported, reason: ItemUseReasonNone,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteOwnedCOSTarget, ItemUsePrerequisiteLivingCOSTarget, ItemUsePrerequisiteMissingCOSResource},
		},
		{
			name:   "COS health potion has no effect value",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestItem(true, 3, 1, 4)},
			family: ItemUseFamilyCOSHealthRecovery, status: ItemUseCapabilityBrokenReference, reason: ItemUseReasonInvalidEffectParameters,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteOwnedCOSTarget, ItemUsePrerequisiteLivingCOSTarget, ItemUsePrerequisiteMissingCOSResource},
		},
		{
			name:   "COS HGP potion",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestPositiveItem(3, 1, 9)},
			family: ItemUseFamilyCOSHGPRecovery, status: ItemUseCapabilitySupported, reason: ItemUseReasonNone,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteOwnedCOSTarget, ItemUsePrerequisiteLivingCOSTarget, ItemUsePrerequisiteGrowthCOSTarget, ItemUsePrerequisiteMissingCOSResource},
		},
		{
			name:   "COS timed service needs skill catalog",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestTimedCOSService(22713)},
			family: ItemUseFamilyCOSTimedService, status: ItemUseCapabilityConditional, reason: ItemUseReasonServiceSkillUnknown,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteOwnedCOSTarget, ItemUsePrerequisiteGrowthCOSTarget, ItemUsePrerequisiteServiceSkillReference},
		},
		{
			name:   "COS timed service supported",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestTimedCOSService(22713), SkillReferences: map[uint32]RefSkill{91: {CodeName: "SKILL_MALL_PET_SKILL_COLD"}}},
			family: ItemUseFamilyCOSTimedService, status: ItemUseCapabilitySupported, reason: ItemUseReasonNone,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteOwnedCOSTarget, ItemUsePrerequisiteGrowthCOSTarget, ItemUsePrerequisiteServiceSkillReference},
		},
		{
			name:   "COS timed service skill missing",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestTimedCOSService(22710), SkillReferences: map[uint32]RefSkill{}},
			family: ItemUseFamilyCOSTimedService, status: ItemUseCapabilityBrokenReference, reason: ItemUseReasonServiceSkillMissing,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteOwnedCOSTarget, ItemUsePrerequisiteAbilityCOSTarget, ItemUsePrerequisiteServiceSkillReference},
		},
		{
			name:   "COS timed service duration invalid",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestItemWithID(22712, 3, 13, 15)},
			family: ItemUseFamilyCOSTimedService, status: ItemUseCapabilityBrokenReference, reason: ItemUseReasonInvalidEffectParameters,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteOwnedCOSTarget, ItemUsePrerequisiteGrowthCOSTarget, ItemUsePrerequisiteServiceSkillReference},
		},
		{
			name:   "unknown COS timed service ID",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestTimedCOSService(99999)},
			family: ItemUseFamilyCOSTimedService, status: ItemUseCapabilityUnsupported, reason: ItemUseReasonUnrecognizedCatalogItem,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteOwnedCOSTarget, ItemUsePrerequisiteServiceSkillReference},
		},
		{
			name:   "COS rental extension",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestPositiveItem(3, 13, 12)},
			family: ItemUseFamilyCOSRentalExtension, status: ItemUseCapabilitySupported, reason: ItemUseReasonNone,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteOwnedCOSTarget, ItemUsePrerequisiteAbilityCOSTarget},
		},
		{
			name:   "COS rental extension duration invalid",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestItem(true, 3, 13, 12)},
			family: ItemUseFamilyCOSRentalExtension, status: ItemUseCapabilityBrokenReference, reason: ItemUseReasonInvalidEffectParameters,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteOwnedCOSTarget, ItemUsePrerequisiteAbilityCOSTarget},
		},
		{
			name:   "COS revival",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestItemWithCode(3, 1, 6, "ITEM_MALL_COS_P_REVIVAL")},
			family: ItemUseFamilyCOSRevival, status: ItemUseCapabilitySupported, reason: ItemUseReasonNone,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteOwnedCOSTarget, ItemUsePrerequisiteDeadCOSTarget},
		},
		{
			name:   "monster mask item data unknown",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestItem(true, 2, 2, 1)},
			family: ItemUseFamilyMonsterMask, status: ItemUseCapabilityConditional, reason: ItemUseReasonItemDataUnknown,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteItemData, ItemUsePrerequisiteCharacterReference},
		},
		{
			name:   "monster mask item data missing",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestItem(true, 2, 2, 1), ItemData: capabilityUint32(0)},
			family: ItemUseFamilyMonsterMask, status: ItemUseCapabilityBrokenReference, reason: ItemUseReasonItemDataMissing,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteItemData, ItemUsePrerequisiteCharacterReference},
		},
		{
			name:   "monster mask character reference missing",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestItem(true, 2, 2, 1), ItemData: capabilityUint32(700), CharacterReferences: map[uint32]RefChar{}},
			family: ItemUseFamilyMonsterMask, status: ItemUseCapabilityBrokenReference, reason: ItemUseReasonReferenceNotFound,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteItemData, ItemUsePrerequisiteCharacterReference},
		},
		{
			name:   "monster mask supported",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestItem(true, 2, 2, 1), ItemData: capabilityUint32(700), CharacterReferences: map[uint32]RefChar{700: validCOS}},
			family: ItemUseFamilyMonsterMask, status: ItemUseCapabilitySupported, reason: ItemUseReasonNone,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteItemData, ItemUsePrerequisiteCharacterReference},
		},
		{
			name:   "collection mapping unknown",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestItem(true, 3, 12, 5)},
			family: ItemUseFamilyCollectionTalisman, status: ItemUseCapabilityConditional, reason: ItemUseReasonCollectionReferenceUnknown,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteCollectionReference, ItemUsePrerequisiteUnregisteredCollectionSlot},
		},
		{
			name:   "collection mapping missing",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestItem(true, 3, 12, 5), CollectionReferenceExists: capabilityBool(false)},
			family: ItemUseFamilyCollectionTalisman, status: ItemUseCapabilityBrokenReference, reason: ItemUseReasonCollectionReferenceMissing,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteCollectionReference, ItemUsePrerequisiteUnregisteredCollectionSlot},
		},
		{
			name:   "collection mapping supported",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestItem(true, 3, 12, 5), CollectionReferenceExists: capabilityBool(true)},
			family: ItemUseFamilyCollectionTalisman, status: ItemUseCapabilitySupported, reason: ItemUseReasonNone,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteCollectionReference, ItemUsePrerequisiteUnregisteredCollectionSlot},
		},
		{
			name:   "character recovery",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestPositiveItem(3, 1, 2)},
			family: ItemUseFamilyCharacterRecovery, status: ItemUseCapabilitySupported, reason: ItemUseReasonNone,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteCharacterNeedsRecovery},
		},
		{
			name:   "character recovery has no effect values",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestItem(true, 3, 1, 3)},
			family: ItemUseFamilyCharacterRecovery, status: ItemUseCapabilityBrokenReference, reason: ItemUseReasonInvalidEffectParameters,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteCharacterNeedsRecovery},
		},
		{
			name:   "random character status cure",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestRandomCure()},
			family: ItemUseFamilyCharacterStatusCure, status: ItemUseCapabilitySupported, reason: ItemUseReasonNone,
		},
		{
			name:   "elemental-duration character cure",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestElementalCure()},
			family: ItemUseFamilyCharacterStatusCure, status: ItemUseCapabilitySupported, reason: ItemUseReasonNone,
		},
		{
			name:   "character cure parameters invalid",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestItem(true, 3, 2, 1)},
			family: ItemUseFamilyCharacterStatusCure, status: ItemUseCapabilityBrokenReference, reason: ItemUseReasonInvalidEffectParameters,
		},
		{
			name:   "COS Grass of Life",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestCOSCure()},
			family: ItemUseFamilyCOSStatusCure, status: ItemUseCapabilitySupported, reason: ItemUseReasonNone,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteOwnedCOSTarget, ItemUsePrerequisiteLivingCOSTarget, ItemUsePrerequisiteGrowthCOSTarget},
		},
		{
			name:   "mall cure bag remains a separate unsupported family",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestPositiveItem(3, 2, 8)},
			family: ItemUseFamilyUnknown, status: ItemUseCapabilityUnsupported, reason: ItemUseReasonNoRuntimeHandler,
		},
		{
			name:   "linked stat buff",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestLinkedItem(buffSkill.CodeName), LinkedSkill: &buffSkill},
			family: ItemUseFamilyLinkedSkillBuff, status: ItemUseCapabilitySupported, reason: ItemUseReasonNone,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteLinkedSkill},
		},
		{
			name:   "linked mall system stat buff",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestLinkedItem(mallSystemBuff.CodeName), LinkedSkill: &mallSystemBuff},
			family: ItemUseFamilyLinkedSkillBuff, status: ItemUseCapabilitySupported, reason: ItemUseReasonNone,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteLinkedSkill},
		},
		{
			name:   "linked mall damage absorption",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestLinkedItem(unresolvedMallEffect.CodeName), LinkedSkill: &unresolvedMallEffect},
			family: ItemUseFamilyLinkedSkillBuff, status: ItemUseCapabilitySupported, reason: ItemUseReasonNone,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteLinkedSkill},
		},
		{
			name:   "linked skill missing",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestLinkedItem("SKILL_TEST_MISSING")},
			family: ItemUseFamilyLinkedSkillEffect, status: ItemUseCapabilityBrokenReference, reason: ItemUseReasonMissingLinkedSkill,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteLinkedSkill},
		},
		{
			name:   "linked skill code mismatch",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestLinkedItem("SKILL_OTHER"), LinkedSkill: &buffSkill},
			family: ItemUseFamilyLinkedSkillEffect, status: ItemUseCapabilityBrokenReference, reason: ItemUseReasonLinkedSkillCodeMismatch,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteLinkedSkill},
		},
		{
			name:   "legacy speed drug",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestLinkedItem(speedSkill.CodeName), LinkedSkill: &speedSkill},
			family: ItemUseFamilySpeedDrug, status: ItemUseCapabilitySupported, reason: ItemUseReasonNone,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteLinkedSkill, ItemUsePrerequisiteExtendableSpeedEffect},
		},
		{
			name:   "legacy speed descriptor invalid",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestLinkedItem(invalidSpeed.CodeName), LinkedSkill: &invalidSpeed},
			family: ItemUseFamilySpeedDrug, status: ItemUseCapabilityBrokenReference, reason: ItemUseReasonInvalidSpeedDescriptor,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteLinkedSkill, ItemUsePrerequisiteExtendableSpeedEffect},
		},
		{
			name:   "linked buff duration invalid",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestLinkedItem(invalidDurationBuff.CodeName), LinkedSkill: &invalidDurationBuff},
			family: ItemUseFamilyLinkedSkillBuff, status: ItemUseCapabilityBrokenReference, reason: ItemUseReasonInvalidBuffDuration,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteLinkedSkill},
		},
		{
			name:   "linked buff modifiers empty",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestLinkedItem(emptyBuff.CodeName), LinkedSkill: &emptyBuff},
			family: ItemUseFamilyLinkedSkillBuff, status: ItemUseCapabilityBrokenReference, reason: ItemUseReasonEmptyBuffModifiers,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteLinkedSkill},
		},
		{
			name:   "linked healing effect is not implemented",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestLinkedItem(healingSkill.CodeName), LinkedSkill: &healingSkill},
			family: ItemUseFamilyLinkedSkillEffect, status: ItemUseCapabilityUnsupported, reason: ItemUseReasonLinkedSkillKindUnsupported,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteLinkedSkill},
		},
		{
			name:   "timed character service",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestCharacterService()},
			family: ItemUseFamilyTimedCharacterService, status: ItemUseCapabilitySupported, reason: ItemUseReasonNone,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteCharacterServiceState},
		},
		{
			name:   "timed character service descriptor invalid",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestItem(true, 3, 13, 4)},
			family: ItemUseFamilyTimedCharacterService, status: ItemUseCapabilityBrokenReference, reason: ItemUseReasonInvalidServiceDescriptor,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteCharacterServiceState},
		},
		{
			name:   "return scroll",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestReturnScroll(true)},
			family: ItemUseFamilyReturnScroll, status: ItemUseCapabilitySupported, reason: ItemUseReasonNone,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteReturnBind},
		},
		{
			name:   "return scroll marker invalid",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestReturnScroll(false)},
			family: ItemUseFamilyReturnScroll, status: ItemUseCapabilityBrokenReference, reason: ItemUseReasonInvalidEffectParameters,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteReturnBind},
		},
		{
			name:   "reverse return scroll",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestPositiveItem(3, 3, 3)},
			family: ItemUseFamilyReverseReturnScroll, status: ItemUseCapabilitySupported, reason: ItemUseReasonNone,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteReverseReturnOption, ItemUsePrerequisiteReverseReturnDestination},
		},
		{
			name:   "reverse return scroll duration invalid",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestItem(true, 3, 3, 3)},
			family: ItemUseFamilyReverseReturnScroll, status: ItemUseCapabilityBrokenReference, reason: ItemUseReasonInvalidEffectParameters,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteReverseReturnOption, ItemUsePrerequisiteReverseReturnDestination},
		},
		{
			name: "option level enhancement scroll",
			input: ItemUseCapabilityInput{RefItem: RefItem{RefObject: RefObject{
				ID: 39364, IsUseable: true, TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 13, TypeID4: 11},
			}}},
			family: ItemUseFamilyOptionLevelUpgrade, status: ItemUseCapabilitySupported, reason: ItemUseReasonNone,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteEventConfiguration, ItemUsePrerequisiteEquipmentTarget},
		},
		{
			name:   "character resurrection uses dedicated request",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestItemWithCode(3, 13, 6, "ITEM_MALL_RESURRECTION_100P")},
			family: ItemUseFamilyCharacterResurrection, status: ItemUseCapabilitySupported, reason: ItemUseReasonNone,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteDeadCharacter, ItemUsePrerequisiteResurrectionRequest},
		},
		{
			name: "Devil Spirit activation",
			input: ItemUseCapabilityInput{RefItem: RefItem{RefObject: RefObject{
				ID: 24405, CodeName: "ITEM_MALL_AVATAR_M_NASRUN", TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 14, TypeID4: 1},
			}, Param1: 2_419_200}},
			family: ItemUseFamilyDevilSpiritActivation, status: ItemUseCapabilitySupported, reason: ItemUseReasonNone,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteDevilSpiritDuration},
		},
		{
			name: "Devil Spirit duration may come from item DATA",
			input: ItemUseCapabilityInput{RefItem: RefItem{RefObject: RefObject{
				ID: 24405, CodeName: "ITEM_MALL_AVATAR_M_NASRUN", TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 14, TypeID4: 1},
			}}},
			family: ItemUseFamilyDevilSpiritActivation, status: ItemUseCapabilityConditional, reason: ItemUseReasonItemDataUnknown,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteDevilSpiritDuration},
		},
		{
			name: "Devil Spirit duration missing",
			input: ItemUseCapabilityInput{RefItem: RefItem{RefObject: RefObject{
				ID: 24405, CodeName: "ITEM_MALL_AVATAR_M_NASRUN", TypeInfo: TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 14, TypeID4: 1},
			}}, ItemData: capabilityUint32(0)},
			family: ItemUseFamilyDevilSpiritActivation, status: ItemUseCapabilityBrokenReference, reason: ItemUseReasonInvalidEffectParameters,
			prerequisites: []ItemUsePrerequisite{ItemUsePrerequisiteDevilSpiritDuration},
		},
		{
			name:   "stock direct-success expendable",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestItem(true, 3, 9, 9)},
			family: ItemUseFamilyDirectSuccessExpendable, status: ItemUseCapabilitySupported, reason: ItemUseReasonNone,
		},
		{
			name:   "stall avatar expendable",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestItem(true, 3, 3, 4)},
			family: ItemUseFamilyDirectSuccessExpendable, status: ItemUseCapabilitySupported, reason: ItemUseReasonNone,
		},
		{
			name:   "no runtime handler",
			input:  ItemUseCapabilityInput{RefItem: capabilityTestItem(true, 3, 8, 9)},
			family: ItemUseFamilyUnknown, status: ItemUseCapabilityUnsupported, reason: ItemUseReasonNoRuntimeHandler,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ClassifyItemUseCapability(test.input)
			if got.Family != test.family || got.Status != test.status || got.Reason != test.reason {
				t.Fatalf("ClassifyItemUseCapability() = %q/%q/%q, want %q/%q/%q", got.Family, got.Status, got.Reason, test.family, test.status, test.reason)
			}
			if !reflect.DeepEqual(got.Prerequisites, test.prerequisites) {
				t.Fatalf("prerequisites = %#v, want %#v", got.Prerequisites, test.prerequisites)
			}
			if got.Ready() != (test.status == ItemUseCapabilitySupported) {
				t.Fatalf("Ready() = %t for status %q", got.Ready(), test.status)
			}
			if got.Implemented() != (test.status != ItemUseCapabilityUnsupported) {
				t.Fatalf("Implemented() = %t for status %q", got.Implemented(), test.status)
			}
			for _, prerequisite := range test.prerequisites {
				if !got.HasPrerequisite(prerequisite) {
					t.Fatalf("HasPrerequisite(%q) = false", prerequisite)
				}
			}
		})
	}
}

func capabilityTestItem(useable bool, typeID2, typeID3, typeID4 int) RefItem {
	return RefItem{RefObject: RefObject{
		ID: 100, IsUseable: useable,
		TypeInfo: TypeInfo{TypeID1: 3, TypeID2: typeID2, TypeID3: typeID3, TypeID4: typeID4},
	}}
}

func capabilityTestPositiveItem(typeID2, typeID3, typeID4 int) RefItem {
	ref := capabilityTestItem(true, typeID2, typeID3, typeID4)
	ref.Param1 = 100
	return ref
}

func capabilityTestItemWithCode(typeID2, typeID3, typeID4 int, code string) RefItem {
	ref := capabilityTestItem(true, typeID2, typeID3, typeID4)
	ref.CodeName = code
	return ref
}

func capabilityTestItemWithID(id uint32, typeID2, typeID3, typeID4 int) RefItem {
	ref := capabilityTestItem(true, typeID2, typeID3, typeID4)
	ref.ID = id
	return ref
}

func capabilityTestCOSInstrument() RefItem {
	ref := capabilityTestItemWithCode(2, 1, 1, "ITEM_COS_P_WOLF")
	ref.Param1Desc = "COS_P_WOLF"
	return ref
}

func capabilityTestTransport() RefItem {
	ref := capabilityTestItemWithCode(3, 3, 2, "ITEM_COS_T_ELEPHANT")
	ref.Param1Desc = "COS_T_ELEPHANT"
	return ref
}

func capabilityTestTimedCOSService(id uint32) RefItem {
	ref := capabilityTestItemWithID(id, 3, 13, 15)
	ref.Param1 = 3_600
	return ref
}

func capabilityTestLinkedItem(skillCode string) RefItem {
	ref := capabilityTestItem(true, 3, 13, 1)
	ref.Param1Desc = skillCode
	return ref
}

func capabilityTestRandomCure() RefItem {
	ref := capabilityTestItem(true, 3, 2, 1)
	ref.Param1, ref.Param2, ref.Param3 = 25_145_280, 9, 100
	return ref
}

func capabilityTestElementalCure() RefItem {
	ref := capabilityTestItem(true, 3, 2, 6)
	ref.Param1, ref.Param2, ref.Param3 = 36, 36, 36
	ref.Param4, ref.Param5, ref.Param6 = 36, 36, 36
	return ref
}

func capabilityTestCOSCure() RefItem {
	ref := capabilityTestElementalCure()
	ref.TypeID4 = 7
	return ref
}

func capabilityTestCharacterService() RefItem {
	ref := capabilityTestItem(true, 3, 13, 4)
	ref.Param1 = 86_400
	ref.Param3 = 10_800_000
	ref.Param4 = 100
	return ref
}

func capabilityTestReturnScroll(validMarker bool) RefItem {
	ref := capabilityTestPositiveItem(3, 3, 1)
	if validMarker {
		ref.Param3Desc = "RESURRECT"
	}
	return ref
}

func capabilityUint32(value uint32) *uint32 { return &value }

func capabilityBool(value bool) *bool { return &value }
