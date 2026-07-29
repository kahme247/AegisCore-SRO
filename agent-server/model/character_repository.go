package model

import (
	"context"
	"database/sql"
	stdErrors "errors"
	"fmt"
	"strings"
	"time"

	"github.com/ferdoran/go-sro/framework/db"
)

type Char struct {
	ID               int
	RefObjID         int
	User             int
	Shard            int
	Name             string
	Scale            byte
	Level            int
	MaxLevel         int
	Exp              int64
	SkillExp         int64
	SkillPoints      int64
	Gold             uint64
	Str              int
	Int              int
	StatPoints       int
	HP               int
	MP               int
	IsDeleting       bool
	PosX             float32
	PosY             float32
	PosZ             float32
	Ctime            time.Time
	Utime            time.Time
	Region           int16
	ReturnTeleportID uint32
	IsGm             bool
}

const (
	SelectCharsByAccountAndShard string = "SELECT ID, REF_OBJ_ID, FK_SHARD, CHAR_NAME, CHAR_SCALE, CURRENT_LEVEL, EXP, SKILL_EXP, STRENGTH, INTELLECT, STAT_POINTS, HP, MP, DELETING, UTIME, FK_REGION FROM `SRO_SHARD`.`CHAR` WHERE FK_USER=? AND FK_SHARD=? ORDER BY CTIME ASC"
	// Character names use the same explicit normalized identity as the
	// migration-backed unique key. Keep the expression in the live queries so
	// the application remains correct both before and after that optional
	// schema migration has been applied; referring directly to its generated
	// column would make pre-migration startup fail.
	SelectCharByNameAndOwner     string = "SELECT ID, REF_OBJ_ID, FK_USER, FK_SHARD, CHAR_NAME, CHAR_SCALE, CURRENT_LEVEL, EXP, SKILL_EXP, STRENGTH, INTELLECT, STAT_POINTS, HP, MP, DELETING, POS_X, POS_Y, POS_Z, CTIME, UTIME, FK_REGION, SKILL_POINTS, GOLD, MAX_LEVEL FROM `SRO_SHARD`.`CHAR` WHERE LOWER(TRIM(CHAR_NAME))=LOWER(TRIM(?)) AND FK_USER=? AND FK_SHARD=? LIMIT 1"
	select_does_charname_exist   string = "SELECT 1 FROM `SRO_SHARD`.`CHAR` WHERE LOWER(TRIM(CHAR_NAME))=LOWER(TRIM(?)) LIMIT 1"
	update_is_deleting_for_owner string = "UPDATE `SRO_SHARD`.`CHAR` SET DELETING=? WHERE LOWER(TRIM(CHAR_NAME))=LOWER(TRIM(?)) AND FK_USER=? AND FK_SHARD=?"
	update_character_position    string = "UPDATE `SRO_SHARD`.`CHAR` SET POS_X=?, POS_Y=?, POS_Z=?, FK_REGION=? WHERE ID=?"
	insert_char                  string = "INSERT INTO `SRO_SHARD`.`CHAR`(REF_OBJ_ID, FK_USER, FK_SHARD, CHAR_NAME, CHAR_SCALE, CURRENT_LEVEL, EXP, SKILL_EXP, STRENGTH, INTELLECT, STAT_POINTS, HP, MP, DELETING, POS_X, POS_Y, POS_Z, FK_REGION, RETURN_TELEPORT_ID) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)"
	SelectIsGm                   string = "SELECT IS_GM FROM `SRO_ACCOUNT`.`USER` WHERE ID=?"
)

var (
	ErrCharacterNotFound      = stdErrors.New("character not found for owner")
	ErrCharacterOwnerRequired = stdErrors.New("character owner is required")
	ErrCharacterShardRequired = stdErrors.New("character shard is required")
	ErrCharacterNameRequired  = stdErrors.New("character name is required")
)

func UpdateCharacterPosition(characterID int, regionID int16, x, y, z float32) error {
	conn := db.OpenConnShard()
	defer conn.Close()

	repository, err := NewCharacterPositionRepository(conn)
	if err != nil {
		return err
	}
	return repository.UpdateCharacterPosition(context.Background(), characterID, regionID, x, y, z)
}

// GetCharacterForOwner is the legacy compatibility adapter for callers that
// have not yet received an injected CharacterRepository. New application
// startup should construct SQLCharacterRepository with long-lived pools and
// call its context-aware method directly.
func GetCharacterForOwner(charName string, userID uint32, shardID uint16) (Char, error) {
	if err := validateCharacterOwner(charName, userID, shardID); err != nil {
		return Char{}, err
	}

	shard := db.OpenConnShard()
	defer shard.Close()
	account := db.OpenConnAccount()
	defer account.Close()

	repository, err := NewCharacterRepository(shard, account)
	if err != nil {
		return Char{}, err
	}
	return repository.GetCharacterForOwner(context.Background(), charName, userID, shardID)
}

// GetCharactersForOwner is the legacy compatibility adapter for callers that
// have not yet received an injected CharacterRepository.
func GetCharactersForOwner(userID uint32, shardID uint16) ([]Char, error) {
	if err := validateCharacterScope(userID, shardID); err != nil {
		return nil, err
	}

	shard := db.OpenConnShard()
	defer shard.Close()

	repository, err := NewCharacterRepository(shard, nil)
	if err != nil {
		return nil, err
	}
	return repository.GetCharactersForOwner(context.Background(), userID, shardID)
}

// SetCharacterDeletionForOwner is the legacy compatibility adapter for callers
// that have not yet received an injected CharacterRepository.
func SetCharacterDeletionForOwner(charName string, userID uint32, shardID uint16, deleting bool) (bool, error) {
	if err := validateCharacterOwner(charName, userID, shardID); err != nil {
		return false, err
	}

	shard := db.OpenConnShard()
	defer shard.Close()
	repository, err := NewCharacterRepository(shard, nil)
	if err != nil {
		return false, err
	}
	return repository.SetCharacterDeletionForOwner(context.Background(), charName, userID, shardID, deleting)
}

func CharacterNameExists(charName string) (bool, error) {
	if strings.TrimSpace(charName) == "" {
		return false, nil
	}

	conn := db.OpenConnShard()
	defer conn.Close()

	var value int
	err := conn.QueryRow(select_does_charname_exist, charName).Scan(&value)
	if stdErrors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check character name: %w", err)
	}
	return value == 1, nil
}

type characterScanner interface {
	Scan(dest ...any) error
}

func scanCharacter(scanner characterScanner) (Char, error) {
	var char Char
	var deleting int
	var posX, posY, posZ sql.NullFloat64
	var ctime, utime sql.NullTime
	var maxLevel sql.NullInt64
	if err := scanner.Scan(
		&char.ID,
		&char.RefObjID,
		&char.User,
		&char.Shard,
		&char.Name,
		&char.Scale,
		&char.Level,
		&char.Exp,
		&char.SkillExp,
		&char.Str,
		&char.Int,
		&char.StatPoints,
		&char.HP,
		&char.MP,
		&deleting,
		&posX,
		&posY,
		&posZ,
		&ctime,
		&utime,
		&char.Region,
		&char.SkillPoints,
		&char.Gold,
		&maxLevel,
	); err != nil {
		return Char{}, err
	}
	char.IsDeleting = deleting != 0
	if posX.Valid {
		char.PosX = float32(posX.Float64)
	}
	if posY.Valid {
		char.PosY = float32(posY.Float64)
	}
	if posZ.Valid {
		char.PosZ = float32(posZ.Float64)
	}
	if ctime.Valid {
		char.Ctime = ctime.Time
	}
	if utime.Valid {
		char.Utime = utime.Time
	}
	if maxLevel.Valid {
		char.MaxLevel = int(maxLevel.Int64)
	}
	return char, nil
}

func validateCharacterOwner(charName string, userID uint32, shardID uint16) error {
	if err := validateCharacterScope(userID, shardID); err != nil {
		return err
	}
	if strings.TrimSpace(charName) == "" {
		return ErrCharacterNameRequired
	}
	return nil
}

func validateCharacterScope(userID uint32, shardID uint16) error {
	if userID == 0 {
		return ErrCharacterOwnerRequired
	}
	if shardID == 0 {
		return ErrCharacterShardRequired
	}
	return nil
}

func CreateChar(char Char, weapon, chest, boots, pants uint32) (bool, int64) {
	conn := db.OpenConnShard()
	defer conn.Close()

	stmt, err := conn.Prepare(insert_char)
	db.CheckError(err)

	res, err := stmt.Exec(char.RefObjID, char.User, char.Shard, char.Name, char.Scale, char.Level, char.Exp, char.SkillExp, char.Str, char.Int, char.StatPoints, char.HP, char.MP, char.IsDeleting, char.PosX, char.PosY, char.PosZ, char.Region)
	db.CheckError(err)

	id, err := res.LastInsertId()
	db.CheckError(err)

	weaponId := CreateItem(weapon, 0)
	chestId := CreateItem(chest, 0)
	bootsId := CreateItem(boots, 0)
	pantsId := CreateItem(pants, 0)

	AddItemToInventory(id, weaponId, SlotPrimaryWeapon)
	AddItemToInventory(id, chestId, SlotChest)
	AddItemToInventory(id, bootsId, SlotBoots)
	AddItemToInventory(id, pantsId, SlotPants)

	return true, id
}
