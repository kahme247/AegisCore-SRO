package character

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"slices"
	"strings"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
	"github.com/sirupsen/logrus"
)

func (h *CombatHandler) awardPlayerKill(ctx context.Context, session *server.Session, player *model.Player, target *model.NPC, targetUID uint32) error {
	if h != nil && h.party != nil {
		recipients := h.party.ExperienceRecipients(player.GetUniqueID())
		if len(recipients) > 1 {
			return h.awardPartyKill(ctx, player, target, targetUID, recipients)
		}
	}
	return h.awardKill(ctx, session, player, target, targetUID, true)
}

func (h *CombatHandler) awardPartyKill(ctx context.Context, killer *model.Player, target *model.NPC, targetUID uint32, recipients []*model.Player) error {
	if h == nil || target == nil || len(recipients) == 0 {
		return ErrCombatTargetInvalid
	}
	if h.rates == nil {
		return fmt.Errorf("gameplay progression rates are unavailable")
	}
	if h.progress == nil {
		return model.ErrCharacterProgressRepositoryUnavailable
	}

	baseReward := target.ExperienceReward()
	rates := h.rates.Current()
	partyExperienceRate := partyExperienceRate(rates)
	experienceShares := service.SharedRewardShares(scaleCombatReward(baseReward, int(partyExperienceRate)), len(recipients))
	skillShares := service.SharedRewardShares(scaleCombatReward(baseReward, int(rates.SkillExperienceRatePercent)), len(recipients))
	jobRecipients := combatJobRecipients(killer, recipients)
	if !h.isJobExperienceTarget(target) {
		jobRecipients = nil
	}
	totalJobLevel := 0
	for _, recipient := range jobRecipients {
		totalJobLevel += recipient.Level
	}
	for index, recipient := range recipients {
		if recipient == nil || recipient.GetSession() == nil {
			continue
		}
		experienceBonus, skillExperienceBonus := recipient.ItemServiceRates(time.Now().UTC())
		reward := scaleCombatReward(experienceShares[index], 100+int(experienceBonus))
		skillReward := scaleCombatReward(skillShares[index], 100+int(skillExperienceBonus))
		progress := recipient.PreviewExperienceAward(h.levels, reward, skillReward)
		if err := h.progress.SaveProgress(ctx, recipient.ID, progress); err != nil {
			return err
		}
		recipient.CommitProgress(progress)
		h.tryAccumulateAcademyExperience(ctx, recipient, reward)
		if err := sendExperienceUpdate(recipient.GetSession(), targetUID, reward, skillReward, progress); err != nil {
			return err
		}
		publishLevelUpAnimation(recipient, progress)
		if slices.Contains(jobRecipients, recipient) {
			base := model.CombatJobExperience(h.levels, recipient.Level, h.combatTargetLevel(target), recipient.GetJob().Type)
			jobReward := model.PartyCombatJobExperience(base, recipient.Level, totalJobLevel, len(jobRecipients))
			h.tryAwardCombatJobExperience(ctx, recipient, jobReward, targetUID)
		}
	}

	// Kill-side effects remain owned by the player whose attack resolved the
	// death. Quest and HWAN records are character-specific and must not be
	// duplicated merely because EXP was shared.
	return h.finishKill(ctx, killer.GetSession(), killer, target, targetUID, true)
}

func partyExperienceRate(rates model.GameplayRates) uint32 {
	if rates.PartyExperienceRatePercent != 0 {
		return rates.PartyExperienceRatePercent
	}
	// Keep injected legacy rate providers safe until they expose the binary's
	// separate party setting. Production rates are loaded from the migrated
	// GAMEPLAY_PROGRESSION_RATES column above.
	return rates.ExperienceRatePercent
}

func (h *CombatHandler) awardKill(ctx context.Context, session *server.Session, player *model.Player, target *model.NPC, targetUID uint32, shareGrowthPetExperience bool) error {
	baseReward := target.ExperienceReward()
	if h.rates == nil {
		return fmt.Errorf("gameplay progression rates are unavailable")
	}
	rates := h.rates.Current()
	experienceRate := int(rates.ExperienceRatePercent)
	skillExperienceRate := int(rates.SkillExperienceRatePercent)
	reward := scaleCombatReward(baseReward, experienceRate)
	skillReward := scaleCombatReward(baseReward, skillExperienceRate)
	experienceBonus, skillExperienceBonus := player.ItemServiceRates(time.Now().UTC())
	reward = scaleCombatReward(reward, 100+int(experienceBonus))
	skillReward = scaleCombatReward(skillReward, 100+int(skillExperienceBonus))
	progress := player.PreviewExperienceAward(h.levels, reward, skillReward)
	if h.progress == nil {
		return model.ErrCharacterProgressRepositoryUnavailable
	}
	if err := h.progress.SaveProgress(ctx, player.ID, progress); err != nil {
		return err
	}
	player.CommitProgress(progress)
	h.tryAccumulateAcademyExperience(ctx, player, reward)
	if err := sendExperienceUpdate(session, targetUID, reward, skillReward, progress); err != nil {
		return err
	}
	publishLevelUpAnimation(player, progress)
	if h.isJobExperienceTarget(target) {
		h.tryAwardCombatJobExperience(ctx, player, model.CombatJobExperience(h.levels, player.Level, h.combatTargetLevel(target), player.GetJob().Type), targetUID)
	}
	return h.finishKill(ctx, session, player, target, targetUID, shareGrowthPetExperience)
}

func (h *CombatHandler) tryAccumulateAcademyExperience(ctx context.Context, player *model.Player, amount uint64) {
	if h == nil || h.academyProgress == nil || player == nil || amount == 0 {
		return
	}
	states, err := h.academyProgress.AccumulateAcademyExperience(ctx, player.ID, amount)
	if err != nil {
		logrus.WithError(err).WithField("character_id", player.ID).Warn("academy experience accumulation failed")
		return
	}
	if len(states) != 0 && h.academyBuffs != nil {
		h.academyBuffs(states)
	}
}

func (h *CombatHandler) isJobExperienceTarget(target *model.NPC) bool {
	if h == nil || h.referenceData == nil || target == nil {
		return false
	}
	ref, exists := h.referenceData.Character(target.GetRefObjectID())
	if !exists {
		return false
	}
	code := strings.ToUpper(strings.TrimSpace(ref.CodeName))
	return strings.HasPrefix(code, "MOB_THIEF_NPC_") || strings.HasPrefix(code, "MOB_HUNTER_NPC_")
}

func (h *CombatHandler) combatTargetLevel(target *model.NPC) int {
	if h == nil || h.referenceData == nil || target == nil {
		return 0
	}
	character, exists := h.referenceData.Character(target.GetRefObjectID())
	if !exists {
		return 0
	}
	return character.Level
}

func combatJobRecipients(killer *model.Player, recipients []*model.Player) []*model.Player {
	if killer == nil || !killer.GetJob().Active() {
		return nil
	}
	killerJob := killer.GetJob().Type
	result := make([]*model.Player, 0, len(recipients))
	for _, recipient := range recipients {
		if recipient == nil || recipient.GetSession() == nil || !recipient.GetJob().Active() {
			continue
		}
		if killerJob == model.PlayerJobHunter && recipient.GetJob().Type != model.PlayerJobHunter {
			continue
		}
		result = append(result, recipient)
	}
	return result
}

func (h *CombatHandler) awardCombatJobExperience(ctx context.Context, player *model.Player, amount uint64) error {
	if h == nil || h.jobProgress == nil || player == nil || amount == 0 || !player.GetJob().Active() {
		return nil
	}
	snapshot, err := h.jobProgress.AdvanceJob(ctx, player.ID, amount, 0)
	if err != nil {
		return err
	}
	player.SetJob(model.PlayerJob{Type: snapshot.Type, Alias: snapshot.Alias, Level: snapshot.Level, Experience: snapshot.Experience})
	packet, err := (vsro188.JobUpdateCodec{}).Encode(vsro188.JobState{Type: snapshot.Type, Level: snapshot.Level, Experience: uint32(min(snapshot.Experience, math.MaxUint32))})
	if err != nil {
		return err
	}
	return h.sendPlayerPacket(player.GetSession(), packet)
}

// tryAwardCombatJobExperience preserves the stock update boundary: normal
// character EXP and the rest of the kill lifecycle have independent durable
// owners and must not be rolled back or skipped after a job update fails.
func (h *CombatHandler) tryAwardCombatJobExperience(ctx context.Context, player *model.Player, amount uint64, targetUID uint32) {
	if err := h.awardCombatJobExperience(ctx, player, amount); err != nil {
		characterID := 0
		if player != nil {
			characterID = player.ID
		}
		logrus.WithError(err).WithFields(logrus.Fields{"character_id": characterID, "target_uid": targetUID, "job_experience": amount}).Warn("combat job experience award failed")
	}
}

func (h *CombatHandler) finishKill(ctx context.Context, session *server.Session, player *model.Player, target *model.NPC, targetUID uint32, shareGrowthPetExperience bool) error {
	if err := h.handleNativeQuestInstanceDeath(player, target); err != nil {
		// The native OnDead callback reports action failure to its quest engine;
		// it does not resurrect the already-dead mob or roll back ordinary kill
		// rewards. Keep that boundary here as well.
		logrus.WithError(err).WithField("target_uid", targetUID).Warn("native quest instance teleport submission failed")
	}
	if shareGrowthPetExperience && h.companions != nil {
		if err := h.companions.AwardOwnerKillExperience(ctx, player, target); err != nil {
			logrus.Warnf("growth pet EXP award failed owner=%d target=%d: %v", player.ID, targetUID, err)
		}
	}
	if h.hwan != nil && h.referenceData != nil {
		character, exists := h.referenceData.Character(target.GetRefObjectID())
		if exists {
			reward := model.HwanRewardForMonsterKill(player.Level, character.Level, target.MonsterRarity())
			roll := h.rollHwanPoint
			if roll == nil {
				roll = func(chanceBasisPoints int) bool {
					return chanceBasisPoints >= 10000 || chanceBasisPoints > 0 && rand.Intn(10000) < chanceBasisPoints
				}
			}
			if roll(reward.ChanceBasisPoints) {
				snapshot, changed, err := h.hwan.AwardPoints(ctx, player.ID, reward.Points)
				if err != nil {
					return err
				}
				player.CommitHwanSnapshot(snapshot)
				if changed {
					points := model.BuildHwanPointUpdatePacket(snapshot.Points, targetUID)
					if err := h.sendPlayerPacket(session, points); err != nil {
						return err
					}
				}
			}
		}
	}
	h.dropsMu.RLock()
	drops := h.drops
	h.dropsMu.RUnlock()
	if drops != nil {
		if _, err := drops.SpawnForKill(ctx, player, target); err != nil {
			logrus.Warnf("monster drop materialization failed target=%d: %v", targetUID, err)
		}
		if resolver, ok := h.quests.(questCollectionDropRepository); ok {
			questDrops, err := resolver.CollectionDropsForKill(ctx, player.ID, target.GetRefObjectID())
			if err != nil {
				return err
			}
			for _, questDrop := range questDrops {
				if _, _, spawnErr := drops.SpawnQuestCollectionItem(ctx, player, target, questDrop); spawnErr != nil {
					logrus.Warnf("quest collection drop materialization failed quest=%d target=%d: %v", questDrop.QuestID, targetUID, spawnErr)
				}
			}
		}
	}
	if h.quests == nil {
		return nil
	}
	if err := h.recordUniqueDead(ctx, session, player, target.GetRefObjectID(), targetUID); err != nil {
		return err
	}
	questProgress, err := h.quests.RecordMobKill(ctx, player.ID, target.GetRefObjectID(), target.MonsterRarity())
	if err != nil {
		return err
	}
	for _, progress := range questProgress {
		if !progress.Changed {
			continue
		}
		player.CommitQuestMutation(progress.Quest)
		update, buildErr := buildQuestStatePacket(progress.Quest)
		if buildErr != nil {
			return buildErr
		}
		if h.send != nil {
			if sendErr := h.send(session, update); sendErr != nil {
				return sendErr
			}
		} else if sendErr := session.Send(update); sendErr != nil {
			return sendErr
		}
	}
	return nil
}

func (h *CombatHandler) handleNativeQuestInstanceDeath(player *model.Player, target *model.NPC) error {
	if h == nil || player == nil || target == nil || h.questInstances == nil || h.instanceTeleport == nil {
		return nil
	}
	scope := model.GameWorldScope(player.GetGameWorldID())
	if !scope.ValidInstance() || target.GetGameWorldID() != uint32(scope) {
		return nil
	}
	instance, err := h.questInstances.Instance(scope)
	if err != nil {
		return err
	}
	variables, err := h.questInstances.Variables(scope)
	if err != nil {
		return err
	}
	elapsed := variables[model.NativeQuestInstanceElapsedTimeKey(instance.QuestID)]
	stagePlan, stageMatched, err := h.questInstances.PrepareNativeBaekryeongStageDeath(scope, target.GetName(), elapsed)
	if err != nil {
		return err
	}
	if stageMatched {
		_, err = h.questInstances.CommitNativeBaekryeongStageDeath(stagePlan)
		return err
	}
	plan, matched, err := h.questInstances.PrepareNativeQuestInstanceDeath(scope, instance.QuestID, target.GetName(), elapsed)
	if err != nil || !matched {
		return err
	}
	return h.instanceTeleport.SubmitNativeQuestInstanceTeleport(h.questInstances, plan, player)
}

func (h *CombatHandler) recordUniqueDead(ctx context.Context, session *server.Session, player *model.Player, monsterRefObjectID, targetUID uint32) error {
	uniqueDead, ok := h.quests.(questUniqueDeadRepository)
	if !ok || player == nil {
		return nil
	}
	result, granted, grantErr := uniqueDead.GrantUniqueDeadMissionItem(ctx, player.ID, monsterRefObjectID, player.GetNavmeshPosition().Region.ID)
	if grantErr != nil {
		if !errors.Is(grantErr, model.ErrQuestInventoryFull) {
			return grantErr
		}
		logrus.WithFields(logrus.Fields{"character_id": player.ID, "target_uid": targetUID}).Info("UNIQUE_DEAD quest item grant skipped: inventory full")
	} else if granted {
		player.CommitQuestItemGrant(model.QuestItemGrantResult{Quest: result.Quest, Inventory: result.Inventory, Added: result.Added})
		if result.RemovedSlot != nil {
			if sendErr := h.sendPlayerPacket(session, buildQuestItemRemoved(*result.RemovedSlot)); sendErr != nil {
				return sendErr
			}
			if *result.RemovedSlot != result.Added.Slot {
				if remaining, exists := result.Inventory.Items[*result.RemovedSlot]; exists {
					if sendErr := h.sendPlayerPacket(session, buildQuestItemAdded(model.QuestItemMutation{Slot: *result.RemovedSlot, Item: remaining})); sendErr != nil {
						return sendErr
					}
				}
			}
		}
		if sendErr := h.sendPlayerPacket(session, buildQuestItemAdded(result.Added)); sendErr != nil {
			return sendErr
		}
		update, buildErr := buildQuestStatePacket(result.Quest)
		if buildErr != nil {
			return buildErr
		}
		if sendErr := h.sendPlayerPacket(session, update); sendErr != nil {
			return sendErr
		}
	}
	return nil
}

// AwardCOSKill routes a growth-pet kill through the owner's normal progression,
// HWAN, and quest pipeline while retaining the killed mob as the EXP source.
func (h *CombatHandler) AwardCOSKill(ctx context.Context, owner *model.Player, target *model.NPC) error {
	if h == nil || owner == nil || owner.Session == nil || target == nil {
		return ErrCombatTargetInvalid
	}
	return h.awardKill(ctx, owner.Session, owner, target, target.GetUniqueID(), false)
}

func sendExperienceUpdate(session *server.Session, sourceUID uint32, gained, gainedSkillExperience uint64, progress model.ProgressionResult) error {
	return session.Send(buildExperienceUpdate(sourceUID, gained, gainedSkillExperience, progress))
}

func publishLevelUpAnimation(player *model.Player, progress model.ProgressionResult) {
	if player == nil || progress.LevelsGained <= 0 {
		return
	}
	packet := model.BuildLevelUpAnimationPacket(player.GetUniqueID())
	player.SendToSelfAndKnown(&packet)
}

func scaleCombatReward(base uint64, percent int) uint64 {
	if percent < 0 {
		percent = 100
	}
	if percent == 0 || base == 0 {
		return 0
	}
	if base > math.MaxUint64/uint64(percent) {
		return math.MaxUint64
	}
	return base * uint64(percent) / 100
}
