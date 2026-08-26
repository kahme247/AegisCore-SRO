package lobby

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

// CharacterStore is the ownership boundary used by network-facing lobby
// handlers. Implementations must treat an unknown and a foreign character as
// the same not-found result.
type CharacterStore interface {
	CharacterForOwner(ctx context.Context, name string, userID uint32, shardID uint16) (model.Char, error)
	CharactersForOwner(ctx context.Context, userID uint32, shardID uint16) ([]model.Char, error)
	SetDeletionForOwner(ctx context.Context, name string, userID uint32, shardID uint16, deleting bool) (bool, error)
	CharacterNameExists(ctx context.Context, name string) (bool, error)
}

type deadCharacterJoinRecoveryStore interface {
	RecoverDeadCharacterForJoin(context.Context, int, uint32, uint16, int, int) (bool, error)
}

type expiredCharacterDeletionStore interface {
	PurgeExpiredCharacters(context.Context, time.Time, int) (int, error)
}

// CharacterInventoryStore is the narrow inventory-read dependency required
// during character selection. It deliberately excludes mutations: inventory
// writes stay on the existing kernel until their transaction boundary exists.
type CharacterInventoryStore interface {
	GetCharacterInventory(ctx context.Context, characterID int64) (model.Inventory, error)
}

type modelCharacterStore struct{}

// modelCharacterStore preserves the legacy constructor only for callers that
// have not yet completed application-startup pool wiring. Production Agent
// startup uses repositoryCharacterStore below instead.
func (modelCharacterStore) CharacterForOwner(_ context.Context, name string, userID uint32, shardID uint16) (model.Char, error) {
	return model.GetCharacterForOwner(name, userID, shardID)
}

func (modelCharacterStore) CharactersForOwner(_ context.Context, userID uint32, shardID uint16) ([]model.Char, error) {
	return model.GetCharactersForOwner(userID, shardID)
}

func (modelCharacterStore) SetDeletionForOwner(_ context.Context, name string, userID uint32, shardID uint16, deleting bool) (bool, error) {
	return model.SetCharacterDeletionForOwner(name, userID, shardID, deleting)
}

func (modelCharacterStore) CharacterNameExists(_ context.Context, name string) (bool, error) {
	return model.CharacterNameExists(name)
}

// repositoryCharacterStore adapts the context-aware model repository to the
// lobby boundary. Name checks remain a convenience preflight; the injected
// creator classifies the authoritative uniqueness conflict inside its
// transaction so a check/create race cannot create partial state.
type repositoryCharacterStore struct {
	repository model.CharacterRepository
}

func (s repositoryCharacterStore) CharacterForOwner(ctx context.Context, name string, userID uint32, shardID uint16) (model.Char, error) {
	if s.repository == nil {
		return model.Char{}, model.ErrCharacterRepositoryUnavailable
	}
	return s.repository.GetCharacterForOwner(ctx, name, userID, shardID)
}

func (s repositoryCharacterStore) RecoverDeadCharacterForJoin(ctx context.Context, characterID int, userID uint32, shardID uint16, hp, mp int) (bool, error) {
	recovery, ok := s.repository.(model.CharacterJoinRecoveryRepository)
	if !ok {
		return false, model.ErrCharacterRepositoryUnavailable
	}
	return recovery.RecoverDeadCharacterForJoin(ctx, characterID, userID, shardID, hp, mp)
}

func (s repositoryCharacterStore) CharactersForOwner(ctx context.Context, userID uint32, shardID uint16) ([]model.Char, error) {
	if s.repository == nil {
		return nil, model.ErrCharacterRepositoryUnavailable
	}
	return s.repository.GetCharactersForOwner(ctx, userID, shardID)
}

func (s repositoryCharacterStore) PurgeExpiredCharacters(ctx context.Context, now time.Time, limit int) (int, error) {
	purger, ok := s.repository.(interface {
		PurgeExpiredCharacters(context.Context, time.Time, int) (int, error)
	})
	if !ok {
		return 0, model.ErrCharacterRepositoryUnavailable
	}
	return purger.PurgeExpiredCharacters(ctx, now, limit)
}

func (s repositoryCharacterStore) SetDeletionForOwner(ctx context.Context, name string, userID uint32, shardID uint16, deleting bool) (bool, error) {
	if s.repository == nil {
		return false, model.ErrCharacterRepositoryUnavailable
	}
	return s.repository.SetCharacterDeletionForOwner(ctx, name, userID, shardID, deleting)
}

func (s repositoryCharacterStore) CharacterNameExists(ctx context.Context, name string) (bool, error) {
	if s.repository == nil {
		return false, model.ErrCharacterRepositoryUnavailable
	}
	return s.repository.CharacterNameExists(ctx, name)
}

// repositoryInventoryStore adapts the context-aware inventory read repository
// to the narrow lobby dependency without making database ownership visible to
// packet handlers.
type repositoryInventoryStore struct {
	repository model.InventoryRepository
}

func (s repositoryInventoryStore) GetCharacterInventory(ctx context.Context, characterID int64) (model.Inventory, error) {
	if s.repository == nil {
		return model.Inventory{}, model.ErrInventoryRepositoryUnavailable
	}
	return s.repository.GetCharacterInventory(ctx, characterID)
}

// modelInventoryStore is only the compatibility fallback for direct handler
// construction in old callers and unit tests. New Agent startup injects an
// inventory repository backed by its process-owned shard pool.
type modelInventoryStore struct{}

func (modelInventoryStore) GetCharacterInventory(_ context.Context, characterID int64) (inventory model.Inventory, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("load character inventory: %v", recovered)
		}
	}()
	return model.GetCharacterInventory(characterID), nil
}

// CharacterSelectionHandler handles both character-list actions and join
// requests. The shared lock preserves their original process-wide ordering
// while the registry preserves ordering for each individual session.
type CharacterSelectionHandler struct {
	mu                sync.Mutex
	characters        CharacterStore
	inventory         CharacterInventoryStore
	avatarInventory   model.AvatarInventoryRepository
	quests            model.CharacterQuestRepository
	masteries         model.MasteryRepository
	skills            model.CharacterSkillRepository
	quickSlots        model.CharacterQuickSlotRepository
	speedEffects      model.SpeedEffectRepository
	itemServices      model.ItemServiceRepository
	itemBuffs         model.ItemBuffRepository
	hwan              model.HwanRepository
	movement          model.MovementSettingsRepository
	referenceData     *model.ReferenceDataCatalog
	creationPolicy    model.CharacterCreationPolicyRepository
	timedBuffDuration bool
	creator           model.CharacterCreator
	loadPlayer        func(model.Char, *server.Session) (*model.Player, error)
	world             characterJoinWorld
}

func (h *CharacterSelectionHandler) ConfigureHwanRepository(repository model.HwanRepository) error {
	if h == nil || repository == nil {
		return model.ErrHwanRepositoryUnavailable
	}
	h.hwan = repository
	return nil
}

func (h *CharacterSelectionHandler) ConfigureAvatarInventoryRepository(repository model.AvatarInventoryRepository) error {
	if h == nil || repository == nil {
		return model.ErrAvatarRepositoryUnavailable
	}
	h.avatarInventory = repository
	return nil
}

func (h *CharacterSelectionHandler) ConfigureReferenceData(catalog *model.ReferenceDataCatalog) error {
	if h == nil || catalog == nil {
		return model.ErrReferenceDataRepositoryUnavailable
	}
	h.referenceData = catalog
	return nil
}

func (h *CharacterSelectionHandler) ConfigureCharacterCreationPolicy(policy model.CharacterCreationPolicyRepository) error {
	if h == nil || policy == nil {
		return model.ErrCharacterCreationPolicyUnavailable
	}
	h.creationPolicy = policy
	return nil
}

func (h *CharacterSelectionHandler) ConfigureQuickSlotRepository(quickSlots model.CharacterQuickSlotRepository) error {
	if h == nil || quickSlots == nil {
		return model.ErrCharacterQuickSlotRepositoryUnavailable
	}
	h.quickSlots = quickSlots
	return nil
}

func (h *CharacterSelectionHandler) ConfigureSpeedEffectRepository(effects model.SpeedEffectRepository) error {
	if h == nil || effects == nil {
		return model.ErrItemUseRepositoryUnavailable
	}
	h.speedEffects = effects
	return nil
}

func (h *CharacterSelectionHandler) ConfigureItemServiceRepository(services model.ItemServiceRepository) error {
	if h == nil || services == nil {
		return model.ErrItemUseRepositoryUnavailable
	}
	h.itemServices = services
	return nil
}

func (h *CharacterSelectionHandler) ConfigureItemBuffRepository(buffs model.ItemBuffRepository) error {
	if h == nil || buffs == nil {
		return model.ErrItemUseRepositoryUnavailable
	}
	h.itemBuffs = buffs
	return nil
}

func (h *CharacterSelectionHandler) ConfigureTimedBuffDuration(enabled bool) {
	if h != nil {
		h.timedBuffDuration = enabled
	}
}

// ConfigureMovementSettingsRepository enables database-backed player speed
// loading for production character joins. Direct legacy constructors retain
// their historical constants for source compatibility.
func (h *CharacterSelectionHandler) ConfigureMovementSettingsRepository(movement model.MovementSettingsRepository) error {
	if h == nil || movement == nil {
		return model.ErrMovementSettingsRepositoryUnavailable
	}
	h.movement = movement
	return nil
}

// ConfigureSkillRepository enables persistence-backed skill bootstrap.
// Learning remains a separate domain operation; this dependency owns only
// durable character skill rows loaded during selection.
func (h *CharacterSelectionHandler) ConfigureSkillRepository(skills model.CharacterSkillRepository) error {
	if h == nil || skills == nil {
		return model.ErrCharacterSkillRepositoryUnavailable
	}
	h.skills = skills
	return nil
}

// ConfigureMasteryRepository enables persistence-backed mastery bootstrap.
// Legacy lobby tests still receive the race defaults at level zero.
func (h *CharacterSelectionHandler) ConfigureMasteryRepository(masteries model.MasteryRepository) error {
	if h == nil || masteries == nil {
		return model.ErrMasteryRepositoryUnavailable
	}
	h.masteries = masteries
	return nil
}

// ConfigureQuestRepository enables persistence-backed quest bootstrap. It is
// separate from the legacy constructors so focused lobby tests and old
// embedders remain source compatible.
func (h *CharacterSelectionHandler) ConfigureQuestRepository(quests model.CharacterQuestRepository) error {
	if h == nil || quests == nil {
		return model.ErrCharacterQuestRepositoryUnavailable
	}
	h.quests = quests
	return nil
}

// characterJoinWorld is the narrow world boundary required while a selected
// character is staged. The real world still allocates the entity UID; the
// handler guarantees that a failed bootstrap removes that staged player.
type characterJoinWorld interface {
	AddPlayer(*model.Player) error
	PlayerDisconnected(uint32, string)
}

func NewCharacterSelectionHandler(characters CharacterStore) *CharacterSelectionHandler {
	if characters == nil {
		characters = modelCharacterStore{}
	}
	// Direct construction remains useful for focused tests of read/join/lobby
	// behavior. It deliberately does not install the legacy CreateChar path:
	// a live creation request must receive the injected transaction creator.
	return &CharacterSelectionHandler{characters: characters, inventory: modelInventoryStore{}}
}

// NewCharacterSelectionHandlerWithRepositories constructs the production
// lobby handler from startup-owned repositories. Both dependencies are
// required so selected-character reads cannot silently fall back to an
// operation-scoped connection.
func NewCharacterSelectionHandlerWithRepositories(characters model.CharacterRepository, inventory model.InventoryRepository) (*CharacterSelectionHandler, error) {
	return newCharacterSelectionHandlerWithRepositories(characters, inventory, nil, false)
}

// NewCharacterSelectionHandlerWithRepositoriesAndCreator constructs the
// production lobby handler with all persistence boundaries supplied by Agent
// startup. Character creation is unavailable unless its pool-backed
// transactional creator is injected explicitly.
func NewCharacterSelectionHandlerWithRepositoriesAndCreator(characters model.CharacterRepository, inventory model.InventoryRepository, creator model.CharacterCreator) (*CharacterSelectionHandler, error) {
	return newCharacterSelectionHandlerWithRepositories(characters, inventory, creator, true)
}

func newCharacterSelectionHandlerWithRepositories(characters model.CharacterRepository, inventory model.InventoryRepository, creator model.CharacterCreator, requireCreator bool) (*CharacterSelectionHandler, error) {
	if characters == nil {
		return nil, model.ErrCharacterRepositoryUnavailable
	}
	if inventory == nil {
		return nil, model.ErrInventoryRepositoryUnavailable
	}
	if requireCreator && creator == nil {
		return nil, model.ErrCharacterCreationRepositoryUnavailable
	}
	return &CharacterSelectionHandler{
		characters: repositoryCharacterStore{repository: characters},
		inventory:  repositoryInventoryStore{repository: inventory},
		creator:    creator,
	}, nil
}

func (h *CharacterSelectionHandler) loadSelectedPlayer(ctx context.Context, char model.Char, session *server.Session) (*model.Player, error) {
	if h != nil && h.loadPlayer != nil {
		return h.loadPlayer(char, session)
	}
	return h.loadPlayerData(ctx, char, session)
}

func (h *CharacterSelectionHandler) joinWorld() characterJoinWorld {
	if h != nil && h.world != nil {
		return h.world
	}
	return service.GetWorldServiceInstance()
}

func RegisterCharacterSelectionHandlers(target *server.Server) {
	if target == nil {
		return
	}
	handler := NewCharacterSelectionHandler(nil)
	target.RegisterHandler(opcode.JoinLobbyRequest, handler.handleJoin)
	target.RegisterHandler(opcode.LobbyActionRequest, handler.handleAction)
}

// RegisterCharacterSelectionHandlersWithRepositories preserves the
// read-only/direct-test constructor compatibility. It intentionally does not
// install a creation fallback; use
// RegisterCharacterSelectionHandlersWithRepositoriesAndCreator for live
// Agent startup.
func RegisterCharacterSelectionHandlersWithRepositories(target *server.Server, characters model.CharacterRepository, inventory model.InventoryRepository) (*CharacterSelectionHandler, error) {
	if target == nil {
		return nil, errors.New("character selection target server is required")
	}
	handler, err := NewCharacterSelectionHandlerWithRepositories(characters, inventory)
	if err != nil {
		return nil, err
	}
	target.RegisterHandler(opcode.JoinLobbyRequest, handler.handleJoin)
	target.RegisterHandler(opcode.LobbyActionRequest, handler.handleAction)
	return handler, nil
}

// RegisterCharacterSelectionHandlersWithRepositoriesAndCreator installs the
// complete live lobby persistence boundary. The supplied creator owns one
// context-bound transaction for each character and its initial inventory.
func RegisterCharacterSelectionHandlersWithRepositoriesAndCreator(target *server.Server, characters model.CharacterRepository, inventory model.InventoryRepository, creator model.CharacterCreator) (*CharacterSelectionHandler, error) {
	if target == nil {
		return nil, errors.New("character selection target server is required")
	}
	handler, err := NewCharacterSelectionHandlerWithRepositoriesAndCreator(characters, inventory, creator)
	if err != nil {
		return nil, err
	}
	target.RegisterHandler(opcode.JoinLobbyRequest, handler.handleJoin)
	target.RegisterHandler(opcode.LobbyActionRequest, handler.handleAction)
	return handler, nil
}

type characterOwnerScope struct {
	UserID  uint32
	ShardID uint16
}

func characterOwner(session *server.Session) (characterOwnerScope, error) {
	if session == nil {
		return characterOwnerScope{}, errors.New("character request has no session")
	}
	if !session.IsAuthenticated() {
		return characterOwnerScope{}, errors.New("character request arrived before authentication")
	}
	if session.UserContext.UserID == 0 {
		return characterOwnerScope{}, model.ErrCharacterOwnerRequired
	}
	if session.UserContext.ShardID == 0 {
		return characterOwnerScope{}, model.ErrCharacterShardRequired
	}
	return characterOwnerScope{UserID: session.UserContext.UserID, ShardID: session.UserContext.ShardID}, nil
}
