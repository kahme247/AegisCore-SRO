package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/go-sql-driver/mysql"
)

const mysqlDuplicateEntryErrorCode uint16 = 1062

var (
	// ErrCharacterCreationRepositoryUnavailable means production startup did
	// not provide the long-lived shard pool required to create a character.
	// Network handlers must fail closed instead of opening a one-off connection.
	ErrCharacterCreationRepositoryUnavailable = errors.New("character creation repository is unavailable")
	// ErrCharacterCreationDuplicate classifies a storage uniqueness conflict
	// without making callers parse a database-driver error string.
	ErrCharacterCreationDuplicate                = errors.New("character creation conflicts with existing data")
	ErrCharacterCreationReferenceObjectRequired  = errors.New("character creation reference object is required")
	ErrCharacterCreationStarterReferenceRequired = errors.New("character creation starter item reference is required")
	ErrCharacterCreationIDRequired               = errors.New("character creation did not return an ID")
	ErrCharacterCreationDuplicateStarterSlot     = errors.New("character creation contains duplicate starter inventory slots")
	ErrCharacterCreationDuplicateStarterSkill    = errors.New("character creation contains duplicate starter skills")
	ErrCharacterCreationDuplicateStarterMastery  = errors.New("character creation contains duplicate starter masteries")
	ErrCharacterCreationStarterMasteryInvalid    = errors.New("character creation starter mastery is invalid")
)

// CharacterStarterItem is one caller-supplied reference item that must be
// created and linked to the new character's normal inventory. It deliberately
// does not encode a starter-kit policy: the lobby maps the exact legacy
// request fields to their existing slots, and this repository persists that
// supplied list atomically.
type CharacterStarterItem struct {
	ReferenceItemID uint32
	Slot            byte
}

// CharacterCreator is the narrow creation boundary used by the lobby. The
// implementation owns a single transaction for the character, item, and
// inventory-link rows; handlers never receive a transaction or a raw pool.
type CharacterCreator interface {
	CreateCharacter(ctx context.Context, character Char, starterItems []CharacterStarterItem) (int64, error)
}

// CharacterSkillCreator extends the established creation API for callers that
// need starter skill rows committed in the same transaction. CharacterCreator
// remains unchanged so existing embedders retain source compatibility.
type CharacterSkillCreator interface {
	CharacterCreator
	CreateCharacterWithSkills(ctx context.Context, character Char, starterItems []CharacterStarterItem, starterSkills []CharacterSkill) (int64, error)
}

// CharacterProgressCreator is retained for compatibility with callers that
// explicitly supply mastery rows. Production creation leaves starterMasteries
// empty so the database reference catalog remains the source of truth.
type CharacterProgressCreator interface {
	CharacterSkillCreator
	CreateCharacterWithProgress(ctx context.Context, character Char, starterItems []CharacterStarterItem, starterSkills []CharacterSkill, starterMasteries []MasteryProgress) (int64, error)
}

// CharacterCreationError identifies the failed transaction stage while
// retaining the underlying driver or context error for errors.Is/errors.As.
type CharacterCreationError struct {
	Operation string
	Err       error
}

func (e *CharacterCreationError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("character creation %s: %v", e.Operation, e.Err)
}

func (e *CharacterCreationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// characterCreationDuplicateError preserves the driver error while exposing a
// stable repository-level classification to callers.
type characterCreationDuplicateError struct {
	err error
}

func (e *characterCreationDuplicateError) Error() string {
	if e == nil {
		return ErrCharacterCreationDuplicate.Error()
	}
	return fmt.Sprintf("%s: %v", ErrCharacterCreationDuplicate, e.err)
}

func (e *characterCreationDuplicateError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.err
}

func (*characterCreationDuplicateError) Is(target error) bool {
	return target == ErrCharacterCreationDuplicate
}

// SQLCharacterCreationRepository creates characters over the Agent-owned
// shard *sql.DB. *sql.DB is intentional here: this boundary must begin one
// context-bound transaction rather than accepting an arbitrary DBTX that
// cannot provide BeginTx.
type SQLCharacterCreationRepository struct {
	shard *sql.DB
}

var _ CharacterCreator = (*SQLCharacterCreationRepository)(nil)
var _ CharacterSkillCreator = (*SQLCharacterCreationRepository)(nil)
var _ CharacterProgressCreator = (*SQLCharacterCreationRepository)(nil)

// NewCharacterCreationRepository constructs the production transaction
// boundary from the Agent's already-opened shard pool.
func NewCharacterCreationRepository(shard *sql.DB) (*SQLCharacterCreationRepository, error) {
	if shard == nil {
		return nil, &CharacterCreationError{
			Operation: "configure shard database",
			Err:       ErrCharacterCreationRepositoryUnavailable,
		}
	}
	return &SQLCharacterCreationRepository{shard: shard}, nil
}

// CreateCharacter inserts the character and every supplied starter item plus
// its inventory link under one BeginTx transaction. A failed write always
// rolls back the complete creation attempt before the error reaches callers.
func (r *SQLCharacterCreationRepository) CreateCharacter(ctx context.Context, character Char, starterItems []CharacterStarterItem) (int64, error) {
	return r.CreateCharacterWithSkills(ctx, character, starterItems, nil)
}

// CreateCharacterWithSkills preserves CreateCharacter's transaction and adds
// the supplied skill rows before commit.
func (r *SQLCharacterCreationRepository) CreateCharacterWithSkills(ctx context.Context, character Char, starterItems []CharacterStarterItem, starterSkills []CharacterSkill) (int64, error) {
	return r.CreateCharacterWithProgress(ctx, character, starterItems, starterSkills, nil)
}

// CreateCharacterWithProgress commits inventory, skill, and mastery defaults
// under the character creation transaction used by the original database.
func (r *SQLCharacterCreationRepository) CreateCharacterWithProgress(ctx context.Context, character Char, starterItems []CharacterStarterItem, starterSkills []CharacterSkill, starterMasteries []MasteryProgress) (int64, error) {
	if r == nil || r.shard == nil {
		return 0, &CharacterCreationError{
			Operation: "begin character creation transaction",
			Err:       ErrCharacterCreationRepositoryUnavailable,
		}
	}
	if err := validateCharacterCreationInput(character, starterItems, starterSkills, starterMasteries); err != nil {
		return 0, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return 0, newCharacterCreationError("begin character creation transaction", err)
	}

	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return 0, newCharacterCreationError("begin character creation transaction", err)
	}
	committed := false
	defer func() {
		if !committed {
			// The original write error is more useful than a cleanup failure. A
			// transaction that was already ended by Commit simply returns
			// sql.ErrTxDone here, which is intentionally ignored.
			_ = tx.Rollback()
		}
	}()

	characterID, err := insertCharacterInTransaction(ctx, tx, character)
	if err != nil {
		return 0, err
	}

	for _, starterItem := range starterItems {
		itemID, err := insertStarterItemInTransaction(ctx, tx, starterItem.ReferenceItemID)
		if err != nil {
			return 0, err
		}
		if err := insertStarterInventoryLinkInTransaction(ctx, tx, characterID, itemID, starterItem.Slot); err != nil {
			return 0, err
		}
	}
	if len(starterSkills) == 0 {
		if err := insertReferenceSkillsInTransaction(ctx, tx, characterID); err != nil {
			return 0, err
		}
	} else {
		for _, starterSkill := range starterSkills {
			if err := insertStarterSkillInTransaction(ctx, tx, characterID, starterSkill); err != nil {
				return 0, err
			}
		}
	}
	if len(starterMasteries) == 0 {
		if err := insertReferenceMasteriesInTransaction(ctx, tx, characterID); err != nil {
			return 0, err
		}
	} else {
		for _, starterMastery := range starterMasteries {
			if err := insertStarterMasteryInTransaction(ctx, tx, characterID, starterMastery); err != nil {
				return 0, err
			}
		}
	}
	if err := insertReferenceQuestsInTransaction(ctx, tx, characterID); err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, newCharacterCreationError("commit character creation transaction", err)
	}
	committed = true
	return characterID, nil
}

func validateCharacterCreationInput(character Char, starterItems []CharacterStarterItem, starterSkills []CharacterSkill, starterMasteries []MasteryProgress) error {
	if character.RefObjID <= 0 {
		return &CharacterCreationError{Operation: "validate character", Err: ErrCharacterCreationReferenceObjectRequired}
	}
	if character.User <= 0 {
		return &CharacterCreationError{Operation: "validate character owner", Err: ErrCharacterOwnerRequired}
	}
	if character.Shard <= 0 {
		return &CharacterCreationError{Operation: "validate character shard", Err: ErrCharacterShardRequired}
	}
	if strings.TrimSpace(character.Name) == "" {
		return &CharacterCreationError{Operation: "validate character name", Err: ErrCharacterNameRequired}
	}

	seenSlots := make(map[byte]struct{}, len(starterItems))
	for _, starterItem := range starterItems {
		if starterItem.ReferenceItemID == 0 {
			return &CharacterCreationError{Operation: "validate starter item reference", Err: ErrCharacterCreationStarterReferenceRequired}
		}
		if err := validateInventorySlot(starterItem.Slot); err != nil {
			return &CharacterCreationError{Operation: "validate starter inventory slot", Err: err}
		}
		if _, exists := seenSlots[starterItem.Slot]; exists {
			return &CharacterCreationError{Operation: "validate starter inventory slot", Err: ErrCharacterCreationDuplicateStarterSlot}
		}
		seenSlots[starterItem.Slot] = struct{}{}
	}
	seenSkills := make(map[uint32]struct{}, len(starterSkills))
	for _, starterSkill := range starterSkills {
		if err := validateCharacterSkill(starterSkill); err != nil {
			return &CharacterCreationError{Operation: "validate starter skill", Err: err}
		}
		if _, exists := seenSkills[starterSkill.ID]; exists {
			return &CharacterCreationError{Operation: "validate starter skill", Err: ErrCharacterCreationDuplicateStarterSkill}
		}
		seenSkills[starterSkill.ID] = struct{}{}
	}
	seenMasteries := make(map[uint32]struct{}, len(starterMasteries))
	for _, starterMastery := range starterMasteries {
		if starterMastery.ID == 0 || starterMastery.Level != 0 {
			return &CharacterCreationError{Operation: "validate starter mastery", Err: ErrCharacterCreationStarterMasteryInvalid}
		}
		if _, exists := seenMasteries[starterMastery.ID]; exists {
			return &CharacterCreationError{Operation: "validate starter mastery", Err: ErrCharacterCreationDuplicateStarterMastery}
		}
		seenMasteries[starterMastery.ID] = struct{}{}
	}
	return nil
}

func insertCharacterInTransaction(ctx context.Context, tx *sql.Tx, character Char) (int64, error) {
	result, err := tx.ExecContext(
		ctx,
		insert_char,
		character.RefObjID,
		character.User,
		character.Shard,
		character.Name,
		character.Scale,
		character.Level,
		character.Exp,
		character.SkillExp,
		character.Str,
		character.Int,
		character.StatPoints,
		character.HP,
		character.MP,
		character.IsDeleting,
		character.PosX,
		character.PosY,
		character.PosZ,
		character.Region,
		character.ReturnTeleportID,
	)
	if err != nil {
		return 0, newCharacterCreationError("insert character", err)
	}
	characterID, err := result.LastInsertId()
	if err != nil {
		return 0, newCharacterCreationError("read character ID", err)
	}
	if characterID <= 0 {
		return 0, &CharacterCreationError{Operation: "read character ID", Err: ErrCharacterCreationIDRequired}
	}
	return characterID, nil
}

func insertStarterItemInTransaction(ctx context.Context, tx *sql.Tx, referenceItemID uint32) (int64, error) {
	// The legacy creation path persisted zero variance for every request
	// supplied starter reference. Keep that exact storage behavior here; item
	// policy and reference validation require separate authoritative data.
	result, err := tx.ExecContext(ctx, InsertItem, referenceItemID, uint64(0))
	if err != nil {
		return 0, newCharacterCreationError("insert starter item", err)
	}
	itemID, err := result.LastInsertId()
	if err != nil {
		return 0, newCharacterCreationError("read starter item ID", err)
	}
	if itemID <= 0 {
		return 0, &CharacterCreationError{Operation: "read starter item ID", Err: ErrCharacterCreationIDRequired}
	}
	return itemID, nil
}

func insertStarterInventoryLinkInTransaction(ctx context.Context, tx *sql.Tx, characterID, itemID int64, slot byte) error {
	if _, err := tx.ExecContext(ctx, InsertInventoryItem, characterID, slot, itemID); err != nil {
		return newCharacterCreationError("insert starter inventory link", err)
	}
	return nil
}

const insertCharacterSkill = "INSERT INTO `SRO_SHARD`.`CHAR_SKILL` (`CHAR_ID`,`SKILL_ID`,`ENABLED`) VALUES (?, ?, ?)"

const insertReferenceCharacterSkills = "INSERT INTO `SRO_SHARD`.`CHAR_SKILL` (`CHAR_ID`,`SKILL_ID`,`ENABLED`) SELECT c.`ID`,defaults.`SKILL_ID`,defaults.`ENABLED` FROM `SRO_SHARD`.`CHAR` c JOIN `SRO_SHARD`.`CHAR_REF_DATA` r ON r.`RefObjID`=c.`REF_OBJ_ID` JOIN `SRO_SHARD`.`REF_CHAR_DEFAULT_SKILL` defaults ON defaults.`RACE` IN (r.`Country`,3) WHERE c.`ID`=?"

func insertReferenceSkillsInTransaction(ctx context.Context, tx *sql.Tx, characterID int64) error {
	if _, err := tx.ExecContext(ctx, insertReferenceCharacterSkills, characterID); err != nil {
		return newCharacterCreationError("insert reference starter skills", err)
	}
	return nil
}

func insertStarterSkillInTransaction(ctx context.Context, tx *sql.Tx, characterID int64, skill CharacterSkill) error {
	if _, err := tx.ExecContext(ctx, insertCharacterSkill, characterID, skill.ID, skill.Enabled); err != nil {
		return newCharacterCreationError("insert starter skill", err)
	}
	return nil
}

const insertCharacterMastery = "INSERT INTO `SRO_SHARD`.`CHAR_MASTERY` (`CHAR_ID`,`MASTERY_ID`,`LEVEL`) VALUES (?, ?, ?)"

const insertReferenceCharacterMasteries = "INSERT INTO `SRO_SHARD`.`CHAR_MASTERY` (`CHAR_ID`,`MASTERY_ID`,`LEVEL`) SELECT c.`ID`,defaults.`MASTERY_ID`,defaults.`LEVEL` FROM `SRO_SHARD`.`CHAR` c JOIN `SRO_SHARD`.`CHAR_REF_DATA` r ON r.`RefObjID`=c.`REF_OBJ_ID` JOIN `SRO_SHARD`.`REF_CHAR_DEFAULT_MASTERY` defaults ON defaults.`RACE` IN (r.`Country`,3) WHERE c.`ID`=?"

func insertReferenceMasteriesInTransaction(ctx context.Context, tx *sql.Tx, characterID int64) error {
	if _, err := tx.ExecContext(ctx, insertReferenceCharacterMasteries, characterID); err != nil {
		return newCharacterCreationError("insert reference starter masteries", err)
	}
	return nil
}

func insertStarterMasteryInTransaction(ctx context.Context, tx *sql.Tx, characterID int64, mastery MasteryProgress) error {
	if _, err := tx.ExecContext(ctx, insertCharacterMastery, characterID, mastery.ID, mastery.Level); err != nil {
		return newCharacterCreationError("insert starter mastery", err)
	}
	return nil
}

const insertReferenceCharacterQuests = "INSERT INTO `SRO_SHARD`.`CHAR_QUEST` (`FK_CHAR`,`QUEST_ID`,`STATUS`,`ACHIEVEMENT_COUNT`,`QUEST_DATA_1`,`QUEST_DATA_2`) SELECT c.`ID`,defaults.`QUEST_ID`,1,0,0,0 FROM `SRO_SHARD`.`CHAR` c JOIN `SRO_SHARD`.`CHAR_REF_DATA` r ON r.`RefObjID`=c.`REF_OBJ_ID` JOIN `SRO_SHARD`.`REF_CHAR_DEFAULT_QUEST` defaults ON defaults.`RACE` IN (r.`Country`,3) WHERE c.`ID`=? AND defaults.`REQUIRED_LEVEL`=1 AND defaults.`SERVICE`=1"

func insertReferenceQuestsInTransaction(ctx context.Context, tx *sql.Tx, characterID int64) error {
	if _, err := tx.ExecContext(ctx, insertReferenceCharacterQuests, characterID); err != nil {
		return newCharacterCreationError("insert reference starter quests", err)
	}
	return nil
}

func newCharacterCreationError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if isCharacterCreationDuplicate(err) {
		err = &characterCreationDuplicateError{err: err}
	}
	return &CharacterCreationError{Operation: operation, Err: err}
}

func isCharacterCreationDuplicate(err error) bool {
	var mysqlError *mysql.MySQLError
	return errors.As(err, &mysqlError) && mysqlError.Number == mysqlDuplicateEntryErrorCode
}
