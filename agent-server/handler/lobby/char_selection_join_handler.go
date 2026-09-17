package lobby

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/engine/environment"
	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/framework/utils"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
	"github.com/g3n/engine/math32"
	log "github.com/sirupsen/logrus"
)

const (
	OpcodeCharSelectionJoinRequest  uint16 = 0x7001
	OpcodeCharSelectionJoinResponse uint16 = 0xB001

	JOIN_ERROR_CHARACTER_OVER_3           uint16 = 0x405
	JOIN_ERROR_CHARACTER_SELECTWEAPON     uint16 = 0x404
	JOIN_ERROR_CHARACTER_NAME_STRING      uint16 = 0x40C
	JOIN_SMERR_NOT_ALLOWED_CHARNAME       uint16 = 0x40D
	JOIN_MSG_ERROR_ID                     uint16 = 0x410
	JOIN_MSG_ERROR_OVERLAP                uint16 = 0x411
	JOIN_SMERR_MAX_USER_EXCEEDED          uint16 = 0x414
	JOIN_SMERR_INVALID_CHARGEN_INFO       uint16 = 0x403
	JOIN_SMERR_FAILED_TO_CREATE_CHARACTER uint16 = 0x406
	JOIN_SMERR_CANT_FIND_GAMESERVER       uint16 = 0x409
	JOIN_SMERR_CANT_ACCESS_PARENT_SERVER  uint16 = 0x40F
	JOIN_SMERR_FAILED_TO_CREATE_NEW_USER  uint16 = 0x412
	JOIN_SMERR_FAILED_TO_ENTERLOBBY       uint16 = 0x415

	legacyPlayerWalkSpeed float32 = 16
	legacyPlayerRunSpeed  float32 = 50
)

func (h *CharacterSelectionHandler) handleJoin(ctx context.Context, session *server.Session, packet network.Packet) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	owner, ownerErr := characterOwner(session)
	if ownerErr != nil {
		return sendJoinResult(session, ResultFalse, JOIN_SMERR_CANT_ACCESS_PARENT_SERVER)
	}
	if !session.EnterLobby() {
		return fmt.Errorf("character join is invalid while session is %s", session.LifecyclePhase())
	}

	request, err := (vsro188.CharacterJoinRequestCodec{}).Decode(packet)
	if err != nil {
		return sendJoinResult(session, ResultFalse, JOIN_ERROR_CHARACTER_NAME_STRING)
	}
	charName := request.Name
	char, err := h.characters.CharacterForOwner(ctx, charName, owner.UserID, owner.ShardID)
	if err != nil || char.ID == 0 || char.IsDeleting || char.User != int(owner.UserID) || char.Shard != int(owner.ShardID) {
		// Unknown, foreign, and deleting characters all receive the same
		// rejection. Do not instantiate a player or disclose ownership.
		return sendJoinResult(session, ResultFalse, JOIN_MSG_ERROR_ID)
	}
	if char.HP == 0 {
		recovery, ok := h.characters.(deadCharacterJoinRecoveryStore)
		if !ok {
			return sendJoinResult(session, ResultFalse, JOIN_SMERR_FAILED_TO_ENTERLOBBY)
		}
		fullHP := utils.BaseHPOrMP(char.Level, char.Str)
		fullMP := utils.BaseHPOrMP(char.Level, char.Int)
		updated, err := recovery.RecoverDeadCharacterForJoin(ctx, char.ID, owner.UserID, owner.ShardID, fullHP, fullMP)
		if err != nil || !updated {
			log.Warnf("failed to recover dead character %d for join: %v", char.ID, err)
			return sendJoinResult(session, ResultFalse, JOIN_SMERR_FAILED_TO_ENTERLOBBY)
		}
		char, err = h.characters.CharacterForOwner(ctx, charName, owner.UserID, owner.ShardID)
		if err != nil || char.HP != fullHP || char.MP != fullMP {
			return sendJoinResult(session, ResultFalse, JOIN_SMERR_FAILED_TO_ENTERLOBBY)
		}
	}

	player, err := h.loadSelectedPlayer(ctx, char, session)
	if err != nil {
		log.Warnf("rejecting character join for account %d character=%d: %v", owner.UserID, char.ID, err)
		return sendJoinResult(session, ResultFalse, JOIN_SMERR_FAILED_TO_ENTERLOBBY)
	}
	if h.playerPreparer != nil {
		if err := h.playerPreparer.PreparePlayer(ctx, player); err != nil {
			log.Warnf("rejecting character join while loading social state character=%d: %v", char.ID, err)
			return sendJoinResult(session, ResultFalse, JOIN_SMERR_FAILED_TO_ENTERLOBBY)
		}
	}
	if !session.BeginCharacterLoading() {
		return fmt.Errorf("character join could not begin loading while session is %s", session.LifecyclePhase())
	}

	world := h.joinWorld()
	addedToWorld := false
	bootstrapComplete := false
	defer func() {
		if bootstrapComplete {
			return
		}
		// A UID is allocated by WorldService.AddPlayer before bootstrap so the
		// client can receive it in character data. If any response fails, do
		// not leave that staged player visible or retain a selected identity.
		if addedToWorld {
			world.PlayerDisconnected(player.UniqueID, player.CharName)
		}
		clearSelectedCharacter(session)
		session.AbortCharacterLoading()
	}()
	if err := world.AddPlayer(player); err != nil {
		log.Debugf("rejecting duplicate character join for account %d: %v", owner.UserID, err)
		return sendJoinResult(session, ResultFalse, JOIN_MSG_ERROR_OVERLAP)
	}
	addedToWorld = true
	player.SetLifeState(model.Spawning)
	player.BodyState = model.NoStatus
	player.MotionState = model.NoMotion
	session.UserContext.UniqueID = player.UniqueID
	session.UserContext.CharacterID = player.ID
	session.UserContext.CharName = player.CharName
	session.UserContext.RefObjId = player.GetRefObjectID()

	joinResult := buildJoinResult(ResultTrue, 0)
	dataBegin := network.EmptyPacket()
	dataBegin.MessageID = opcode.CharacterDataBegin
	if err := session.SendBatch([]network.Packet{joinResult, dataBegin}); err != nil {
		return err
	}
	if err := sendCharacterBootstrapBody(session, player); err != nil {
		return err
	}
	h.publishRestoredSpeedEffect(player)
	h.publishRestoredItemServices(player)

	bootstrapComplete = true
	log.Infof("sent Agent spawn data for character %s; awaiting GameReady", player.CharName)
	return nil
}

func clearSelectedCharacter(session *server.Session) {
	if session == nil {
		return
	}
	session.UserContext.UniqueID = 0
	session.UserContext.CharacterID = 0
	session.UserContext.CharName = ""
	session.UserContext.RefObjId = 0
}

func (h *CharacterSelectionHandler) loadPlayerData(ctx context.Context, char model.Char, session *server.Session) (*model.Player, error) {
	if session == nil || char.ID == 0 || char.User != int(session.UserContext.UserID) || char.Shard != int(session.UserContext.ShardID) {
		return nil, model.ErrCharacterNotFound
	}

	avatarInventory := model.NewInventory(nil)
	var err error
	if h != nil && h.avatarInventory != nil {
		avatarInventory, err = h.avatarInventory.GetCharacterAvatarInventory(ctx, int64(char.ID))
		if err != nil {
			return nil, err
		}
	}
	inventory, err := h.loadPlayerInventory(ctx, int64(char.ID))
	if err != nil {
		return nil, err
	}
	var quests []model.CharacterQuest
	if h.quests != nil {
		quests, err = h.quests.List(ctx, char.ID)
		if err != nil {
			return nil, fmt.Errorf("load character quests: %w", err)
		}
	}
	refChar, exists := model.RefChars[uint32(char.RefObjID)]
	if !exists {
		return nil, fmt.Errorf("reference character %d is unavailable", char.RefObjID)
	}
	movementSettings, err := h.playerMovementSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("load player movement settings: %w", err)
	}

	world := service.GetWorldServiceInstance()
	region, err := world.GetRegion(char.Region)
	if err != nil {
		return nil, fmt.Errorf("load character region: %w", err)
	}
	offset := math32.NewVector3(char.PosX, char.PosY, char.PosZ)
	cell, err := region.ResolveCell(offset)
	if err != nil {
		return nil, fmt.Errorf("resolve character spawn cell: %w", err)
	}
	// Keep the persisted vertical coordinate. Navmesh height samples use a
	// different vertical scale than the client position protocol.

	player := &model.Player{
		SRObject:        model.NewSRObject(),
		Session:         session,
		ID:              char.ID,
		CharName:        char.Name,
		Scale:           char.Scale,
		Inventory:       inventory,
		AvatarInventory: avatarInventory,
		InventorySize:   char.InventorySize,
		BaseStats: model.BaseStats{
			HP:  char.HP,
			MP:  char.MP,
			Str: char.Str,
			Int: char.Int,
		},
		SkillPoints:    int(char.SkillPoints),
		StatPoints:     char.StatPoints,
		ExpOffset:      uint64(char.Exp),
		SkillExpOffset: uint(char.SkillExp),
		Gold:           char.Gold,
		Quests:         append([]model.CharacterQuest(nil), quests...),
		Level:          char.Level,
		MaxLevel:       char.MaxLevel,
		Mutex:          sync.Mutex{},
		IsGm:           char.IsGm,
	}
	player.Type = model.PlayerType
	player.WalkSpeed = movementSettings.PlayerWalkSpeed
	player.RunSpeed = movementSettings.PlayerRunSpeed
	player.HwanSpeed = 100
	player.RefObjectID = uint32(char.RefObjID)
	player.UniqueID = 0
	player.RtNavmeshPosition = navmeshv2.RtNavmeshPosition{
		Offset:  offset,
		Heading: float32(rand.Intn(0xFFFF)),
		Region:  region.Region,
		Cell:    &cell,
	}
	player.Name = player.CharName
	player.TypeInfo = refChar.TypeInfo
	player.CharKnownObjectList = model.NewCharKnownObjectList(player)
	player.KnownObjectList = player.CharKnownObjectList
	if err := h.loadPlayerMasteries(ctx, player); err != nil {
		return nil, err
	}
	if err := h.loadPlayerSkills(ctx, player); err != nil {
		return nil, err
	}
	if err := h.loadPlayerQuickSlots(ctx, player); err != nil {
		return nil, err
	}
	if err := h.loadPlayerAutoPotionSettings(ctx, player); err != nil {
		return nil, err
	}
	if err := h.loadPlayerSpeedEffect(ctx, player); err != nil {
		return nil, err
	}
	if err := h.loadPlayerItemServices(ctx, player); err != nil {
		return nil, err
	}
	if err := h.loadPlayerItemBuffs(ctx, player); err != nil {
		return nil, err
	}
	if h.hwan != nil {
		snapshot, err := h.hwan.Load(ctx, player.ID)
		if err != nil {
			return nil, fmt.Errorf("load character HWAN state: %w", err)
		}
		player.CommitHwanSnapshot(snapshot)
	}
	return player, nil
}

func (h *CharacterSelectionHandler) loadPlayerItemBuffs(ctx context.Context, player *model.Player) error {
	if player == nil || h == nil || h.itemBuffs == nil {
		return nil
	}
	effects, err := h.itemBuffs.LoadItemBuffs(ctx, int64(player.ID))
	if err != nil {
		return err
	}
	for _, effect := range effects {
		remaining := time.Until(effect.ExpiresAt)
		if remaining <= 0 {
			continue
		}
		player.CommitActiveBuff(effect.SkillID, model.NextActionUID(), remaining, effect.Modifiers)
	}
	return nil
}

func (h *CharacterSelectionHandler) loadPlayerItemServices(ctx context.Context, player *model.Player) error {
	if player == nil || h == nil || h.itemServices == nil {
		return nil
	}
	effects, err := h.itemServices.LoadItemServices(ctx, int64(player.ID))
	if err != nil {
		return err
	}
	for _, effect := range effects {
		player.ApplyItemService(effect)
	}
	return nil
}

func (h *CharacterSelectionHandler) loadPlayerSpeedEffect(ctx context.Context, player *model.Player) error {
	if player == nil || h == nil || h.speedEffects == nil {
		return nil
	}
	effect, exists, err := h.speedEffects.LoadSpeedEffect(ctx, int64(player.ID))
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	player.ApplyMovementSpeedEffect(effect.SkillID, effect.Percent, effect.ExpiresAt, model.NextActionUID())
	return nil
}

func (h *CharacterSelectionHandler) publishRestoredSpeedEffect(player *model.Player) {
	if player == nil {
		return
	}
	skillID, _, expiresAt, generation, token, exists := player.MovementSpeedEffect()
	if !exists {
		return
	}
	remaining := time.Until(expiresAt)
	if remaining <= 0 {
		return
	}
	added := model.BuildBuffAdd(player.GetUniqueID(), skillID, token)
	if h.timedBuffDuration {
		added = model.BuildTimedBuffAdd(player.GetUniqueID(), skillID, token, uint32(remaining.Milliseconds()))
	}
	player.SendToSelfAndKnown(&added)
	player.SendMovementSpeedUpdate()
	delay := remaining
	if delay <= 0 {
		return
	}
	time.AfterFunc(delay, func() {
		if expiredToken, cleared := player.ClearMovementSpeedEffect(generation); cleared {
			removed := model.BuildBuffRemove(expiredToken)
			player.SendToSelfAndKnown(&removed)
			player.SendMovementSpeedUpdate()
		}
	})
}

func (h *CharacterSelectionHandler) publishRestoredItemServices(player *model.Player) {
	if player == nil {
		return
	}
	now := time.Now()
	for _, state := range player.ItemServiceStates(now) {
		remaining := time.Until(state.Effect.ExpiresAt)
		if remaining <= 0 || state.Token == 0 {
			continue
		}
		if model.ItemServiceUsesItemPerk(state.Effect) {
			added := model.BuildItemPerkAdd(player.GetUniqueID(), state.Effect.SourceRefItemID, state.Token, state.Effect.DailyBudget, remaining)
			player.SendToSelfAndKnown(&added)
		}
		if premiumAdded, exists := model.BuildPremiumDailyAdd(state.Token, state.Effect, remaining); exists {
			player.SendToSelf(&premiumAdded)
		}
		if buffAdded, exists := model.BuildItemServiceBuffAdd(player.GetUniqueID(), state.Token, state.Effect, remaining); exists {
			player.SendToSelfAndKnown(&buffAdded)
		}
	}
}

func (h *CharacterSelectionHandler) playerMovementSettings(ctx context.Context) (model.MovementSettings, error) {
	if h == nil || h.movement == nil {
		return model.MovementSettings{
			PlayerWalkSpeed: legacyPlayerWalkSpeed,
			PlayerRunSpeed:  legacyPlayerRunSpeed,
		}, nil
	}
	return h.movement.MovementSettings(ctx)
}

func (h *CharacterSelectionHandler) loadPlayerMasteries(ctx context.Context, player *model.Player) error {
	if player == nil {
		return model.ErrMasteryConflict
	}
	player.Masteries = make(map[uint32]byte)
	if h == nil || h.masteries == nil {
		return nil
	}
	persisted, err := h.masteries.LoadMasteries(ctx, player.ID)
	if err != nil {
		return err
	}
	for id, level := range persisted {
		if id == 0 || int(level) > player.Level {
			return fmt.Errorf("invalid persisted mastery %d level %d for character %d", id, level, player.ID)
		}
		player.Masteries[id] = level
	}
	return nil
}

func (h *CharacterSelectionHandler) loadPlayerSkills(ctx context.Context, player *model.Player) error {
	if player == nil {
		return model.ErrCharacterSkillCharacterIDRequired
	}
	if h == nil || h.skills == nil {
		player.SetSkills(nil)
		return nil
	}
	skills, err := h.skills.LoadSkills(ctx, player.ID)
	if err != nil {
		return err
	}
	player.SetSkills(skills)
	player.RecalculatePassiveSkillModifiers(h.referenceData)
	return nil
}

func (h *CharacterSelectionHandler) loadPlayerQuickSlots(ctx context.Context, player *model.Player) error {
	if player == nil {
		return model.ErrCharacterQuickSlotCharacterIDRequired
	}
	if h == nil || h.quickSlots == nil {
		player.SetQuickSlots(nil)
		return nil
	}
	slots, err := h.quickSlots.LoadQuickSlots(ctx, player.ID)
	if err != nil {
		return err
	}
	player.SetQuickSlots(slots)
	return nil
}

func (h *CharacterSelectionHandler) loadPlayerAutoPotionSettings(ctx context.Context, player *model.Player) error {
	if player == nil || player.ID <= 0 {
		return model.ErrAutoPotionCharacterIDRequired
	}
	if h == nil || h.autoPotion == nil {
		player.SetAutoPotionSettings(model.AutoPotionSettings{}, false)
		return nil
	}
	settings, present, err := h.autoPotion.LoadAutoPotionSettings(ctx, player.ID)
	if err != nil {
		return err
	}
	player.SetAutoPotionSettings(settings, present)
	return nil
}

func (h *CharacterSelectionHandler) loadPlayerInventory(ctx context.Context, charID int64) (model.Inventory, error) {
	if h == nil || h.inventory == nil {
		return model.Inventory{}, model.ErrInventoryRepositoryUnavailable
	}
	inventoryDAO, err := h.inventory.GetCharacterInventory(ctx, charID)
	if err != nil {
		return model.Inventory{}, err
	}
	// A selected player owns its mutable inventory state. Snapshot before the
	// value crosses this boundary so a cache or test fake cannot race a
	// concurrent relocation while this join is copying it.
	return inventoryDAO.Snapshot(), nil
}

func sendJoinResult(session *server.Session, result byte, errorCode uint16) error {
	if session == nil {
		return fmt.Errorf("character join has no session")
	}
	return session.Send(buildJoinResult(result, errorCode))
}

func buildJoinResult(result byte, errorCode uint16) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = OpcodeCharSelectionJoinResponse
	packet.WriteByte(result)
	if result == ResultFalse {
		packet.WriteUInt16(errorCode)
	}
	return packet
}

func sendCharacterBootstrap(session *server.Session, player *model.Player) error {
	pDataBegin := network.EmptyPacket()
	pDataBegin.MessageID = opcode.CharacterDataBegin
	if err := session.Send(pDataBegin); err != nil {
		return err
	}
	return sendCharacterBootstrapBody(session, player)
}

func sendCharacterBootstrapBody(session *server.Session, player *model.Player) error {
	traceBootstrap := os.Getenv("SRO_SESSION_TRACE") == "1"
	if traceBootstrap {
		log.Infof("CHAR_BOOTSTRAP_TRACE start character=%s id=%d", player.CharName, player.ID)
	}

	pDataBody := network.EmptyPacket()
	pDataBody.MessageID = opcode.CharacterDataBody
	pDataBody.WriteUInt32(utils.ToSilkroadTime(time.Now()))
	WriteCharDataToPacket(&pDataBody, player)
	if traceBootstrap {
		log.Infof("CHAR_BOOTSTRAP_TRACE character-data character=%s size=%d", player.CharName, pDataBody.PayloadLen())
	}
	WriteInventoryToPacket(&pDataBody, player.Inventory)
	if traceBootstrap {
		log.Infof("CHAR_BOOTSTRAP_TRACE inventory character=%s size=%d", player.CharName, pDataBody.PayloadLen())
	}
	WriteAvatarInventoryToPacket(&pDataBody, player.AvatarInventory)
	pDataBody.WriteByte(0) // Mastery section begin
	masteries := player.MasterySnapshot()
	masteryIDs := make([]uint32, 0, len(masteries))
	for masteryID := range masteries {
		masteryIDs = append(masteryIDs, masteryID)
	}
	sort.Slice(masteryIDs, func(i, j int) bool { return masteryIDs[i] < masteryIDs[j] })
	for _, masteryID := range masteryIDs {
		WriteMasteryOrSkill(&pDataBody, masteryID, masteries[masteryID])
	}
	pDataBody.WriteByte(2) // Mastery section end
	if traceBootstrap {
		log.Infof("CHAR_BOOTSTRAP_TRACE masteries character=%s count=%d size=%d", player.CharName, len(masteryIDs), pDataBody.PayloadLen())
	}
	writeCharacterSkillSection(&pDataBody, player)
	if traceBootstrap {
		log.Infof("CHAR_BOOTSTRAP_TRACE skills character=%s size=%d", player.CharName, pDataBody.PayloadLen())
	}
	if err := model.WriteQuestLog(&pDataBody, player.QuestSnapshot()); err != nil {
		return fmt.Errorf("write character quest log: %w", err)
	}
	if traceBootstrap {
		log.Infof("CHAR_BOOTSTRAP_TRACE quests character=%s size=%d", player.CharName, pDataBody.PayloadLen())
	}
	pDataBody.WriteByte(0)
	pDataBody.WriteUInt32(0)
	pDataBody.WriteUInt32(player.GetUniqueID())
	model.WritePosition(&pDataBody, player.GetNavmeshPosition())
	pDataBody.WriteByte(0) // HasDestination
	pDataBody.WriteByte(1)
	pDataBody.WriteByte(0)
	pDataBody.WriteUInt16(uint16(player.GetNavmeshPosition().Heading))
	pDataBody.WriteByte(byte(model.Spawning))
	pDataBody.WriteByte(0)
	pDataBody.WriteByte(byte(model.NoMotion))
	pDataBody.WriteByte(byte(player.BodyState))
	pDataBody.WriteFloat32(player.GetWalkSpeed())
	pDataBody.WriteFloat32(player.GetRunSpeed())
	pDataBody.WriteFloat32(player.GetHwanSpeed())
	pDataBody.WriteByte(0) // Buff count
	pDataBody.WriteString(player.CharName)
	job := player.GetJob()
	if job.Active() {
		pDataBody.WriteString(job.Alias)
		pDataBody.WriteByte(job.Type)
		pDataBody.WriteByte(job.Level)
	} else {
		// Preserve the established no-job bootstrap representation until the
		// inactive branch is independently captured; active state must not be
		// replaced with these legacy placeholders.
		pDataBody.WriteUInt16(0)
		pDataBody.WriteByte(0)
		pDataBody.WriteByte(1)
	}
	pDataBody.WriteUInt32(0)
	pDataBody.WriteUInt32(0)
	pDataBody.WriteUInt32(0)
	pDataBody.WriteByte(0)
	pDataBody.WriteByte(0)
	pDataBody.WriteByte(0)
	pDataBody.WriteByte(0xFF)
	// Guide bit 0 suppresses the stock client's first-login defaults. Leaving
	// it clear makes vSRO overwrite restored quick slots 1-4 and persist those
	// defaults back through 0x7158 on every relog.
	pDataBody.WriteUInt64(1)
	// Stock vSRO identifies client configuration with the account JID, not the
	// shard character ID.
	pDataBody.WriteUInt32(session.UserContext.UserID)
	if player.IsGm {
		pDataBody.WriteByte(1)
	} else {
		pDataBody.WriteByte(0)
	}
	writeCharacterQuickSlotConfig(&pDataBody, player)
	writeAutoPotionConfig(&pDataBody, player)
	pDataBody.WriteByte(0)
	pDataBody.WriteUInt32(0x00010001)
	pDataBody.WriteByte(0)
	if traceBootstrap {
		log.Infof("CHAR_BOOTSTRAP_TRACE sending-body character=%s size=%d", player.CharName, pDataBody.PayloadLen())
	}
	if err := session.Send(pDataBody); err != nil {
		return err
	}

	pDataEnd := network.EmptyPacket()
	pDataEnd.MessageID = opcode.CharacterDataEnd
	if err := session.Send(pDataEnd); err != nil {
		return err
	}

	celestialPosition := environment.GetCelestialPositionForPlayer(player)
	pCelestialPos := network.EmptyPacket()
	pCelestialPos.MessageID = opcode.CelestialPosition
	pCelestialPos.WriteUInt32(celestialPosition.CharUniqueID)
	pCelestialPos.WriteUInt16(celestialPosition.Moonphase)
	pCelestialPos.WriteByte(celestialPosition.Hour)
	pCelestialPos.WriteByte(celestialPosition.Minute)
	if err := session.Send(pCelestialPos); err != nil {
		return err
	}

	weatherType, intensity := environment.GetCurrentWeather()
	pWeather := environment.BuildWeatherUpdate(weatherType, intensity)
	if err := session.Send(pWeather); err != nil {
		return err
	}

	player.SendStatsUpdate()
	player.SendMovementStateUpdate()

	pReady := network.EmptyPacket()
	pReady.MessageID = 0x3077
	pReady.WriteByte(0)
	pReady.WriteByte(0)
	return session.Send(pReady)
}

func writeCharacterQuickSlotConfig(packet *network.Packet, player *model.Player) {
	slots := player.QuickSlotSnapshot()
	packet.WriteByte(7) // Required vSRO quick-slot configuration format version.
	packet.WriteByte(byte(len(slots)))
	for _, slot := range slots {
		packet.WriteByte(slot.Sequence)
		packet.WriteByte(slot.ContentType)
		packet.WriteUInt32(slot.ContentID)
	}
}

var defaultAutoPotionSettings = model.AutoPotionSettings{0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00}

func writeAutoPotionConfig(packet *network.Packet, player *model.Player) {
	settings, present := player.AutoPotionSettingsSnapshot()
	if !present {
		settings = defaultAutoPotionSettings
	}
	packet.WriteBytes(settings[:])
}

func writeCharacterSkillSection(packet *network.Packet, player *model.Player) {
	packet.WriteByte(0) // Unknown byte between mastery and skill lists.
	for _, skill := range player.SkillSnapshot() {
		if isImplicitBaseAttackSkill(skill.ID) {
			continue
		}
		packet.WriteByte(1)
		packet.WriteUInt32(skill.ID)
		packet.WriteByte(skill.Enabled)
	}
	packet.WriteByte(2) // Skill list terminator.
}

func isImplicitBaseAttackSkill(skillID uint32) bool {
	switch skillID {
	case 1, 2, 40, 70, 8419, 8420, 8421, 9354, 9355, 9944, 10625, 11162, 11526:
		return true
	default:
		return false
	}
}

// SendCharacterBootstrap resends the stock character loading stream after an
// in-server teleport. Character selection and teleport use the same vSRO 1.88
// bootstrap body; only their preceding handshake differs.
func SendCharacterBootstrap(session *server.Session, player *model.Player) error {
	return sendCharacterBootstrap(session, player)
}

func WriteCharDataToPacket(p *network.Packet, data *model.Player) {
	p.WriteUInt32(data.GetRefObjectID())
	p.WriteByte(byte(data.Scale))
	p.WriteByte(byte(data.Level))
	p.WriteByte(byte(data.MaxLevel))
	p.WriteUInt64(data.ExpOffset)
	p.WriteUInt32(uint32(data.SkillExpOffset))
	p.WriteUInt64(data.Gold)
	p.WriteUInt32(uint32(data.SkillPoints))
	p.WriteUInt16(uint16(data.StatPoints))
	p.WriteByte(data.RemainHwanCount)
	p.WriteUInt32(data.GatheredHwanPoint)
	p.WriteUInt32(uint32(data.BaseStats.HP))
	p.WriteUInt32(uint32(data.BaseStats.MP))
	p.WriteByte(1)
	p.WriteByte(0)
	p.WriteUInt16(0)
	p.WriteUInt32(0)
	p.WriteByte(data.GetHwanLevel())
	p.WriteByte(0)
}

func WriteInventoryToPacket(p *network.Packet, data model.Inventory) {
	data = data.Snapshot()
	p.WriteByte(model.BaseInventorySize)
	p.WriteByte(byte(len(data.Items)))
	for _, slot := range model.SortedInventorySlots(data.Items) {
		item := data.Items[slot]
		p.WriteByte(slot)
		model.WriteRentInfo(p, item)
		p.WriteUInt32(item.GetRefObjectID())
		model.WriteInventoryItem(p, item)
	}
}

func WriteAvatarInventoryToPacket(p *network.Packet, data model.Inventory) {
	data = data.Snapshot()
	p.WriteByte(model.AvatarInventorySize)
	p.WriteByte(byte(len(data.Items)))
	for _, slot := range model.SortedInventorySlots(data.Items) {
		if slot >= model.AvatarInventorySize {
			continue
		}
		item := data.Items[slot]
		p.WriteByte(slot)
		model.WriteRentInfo(p, item)
		p.WriteUInt32(item.GetRefObjectID())
		model.WriteInventoryItem(p, item)
	}
}

func WriteMasteryOrSkill(p *network.Packet, masteryID uint32, masteryLevel byte) {
	p.WriteByte(1)
	p.WriteUInt32(masteryID)
	p.WriteByte(masteryLevel)
}
