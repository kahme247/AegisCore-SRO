package model

import (
	"context"
	"errors"
)

var (
	ErrSkillRestorationInvalid          = errors.New("skill restoration request is invalid")
	ErrSkillRestorationDependency       = errors.New("a learned skill depends on the requested level")
	ErrSkillRestorationPaidPath         = errors.New("paid skill restoration reference data is unavailable")
	ErrSkillRestorationGold             = errors.New("insufficient gold for skill restoration")
	ErrSkillRestorationConflict         = errors.New("skill restoration state conflicts with the request")
	ErrSkillRestorationRepositoryAbsent = errors.New("skill restoration repository is unavailable")
)

const (
	SkillRestorationMallItem  = "ITEM_MALL_SKILL_RESTORATION_POTION"
	SkillRestorationQuestItem = "ITEM_QNO_RM_OLDWOMAN_2_02"
	SkillRestorationPaidItem  = "ITEM_QSP_ALL_POTION_1_01"
)

type SkillWithdrawCommand struct {
	ItemRefID            uint32
	CurrentSkillID       uint32
	ResultingSkillID     uint32
	TargetLevel          byte
	RequiredItemQuantity uint16
	RefundSkillPoints    uint64
	ForbiddenSkillIDs    []uint32
	Paid                 bool
	GoldCost             uint64
	GoldMasteryLevels    []byte
}

type MasteryWithdrawCommand struct {
	ItemRefID            uint32
	MasteryID            uint32
	ExpectedLevel        byte
	TargetLevel          byte
	RequiredItemQuantity uint16
	RefundSkillPoints    uint64
	ForbiddenSkillIDs    []uint32
	Paid                 bool
	GoldCost             uint64
}

type SkillRestorationResult struct {
	ResultingSkillID uint32
	Mastery          MasteryProgress
	SkillPoints      uint32
	ItemSlot         byte
	ItemRemaining    uint16
	Gold             uint64
	GoldCharged      uint64
}

type SkillRestorationRepository interface {
	WithdrawSkill(context.Context, int, SkillWithdrawCommand) (SkillRestorationResult, error)
	WithdrawMastery(context.Context, int, MasteryWithdrawCommand) (SkillRestorationResult, error)
}
