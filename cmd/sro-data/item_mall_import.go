package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Harvi8/AegisCore/framework/config"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

var errItemMallImportUsage = errors.New("usage: sro-data import-item-mall --media <Media directory>")

type itemMallSourceRow struct {
	file   string
	line   int
	fields []string
}

type itemMallPlacement struct {
	shopGroupID uint16
	groupIndex  uint8
	tabIndex    uint8
	slotIndex   uint8
	packageID   uint32
	packageCode string
	itemCode    string
	currency    uint8
	price       uint32
	quantity    uint16
	optLevel    uint8
	variance    uint64
	data        uint32
}

type itemMallGood struct {
	itemMallPlacement
	itemID int32
}

type itemMallMapping struct {
	left, right string
	index       uint8
	row         itemMallSourceRow
}

type itemMallTab struct {
	group string
	index uint8
	row   itemMallSourceRow
}

type itemMallPackage struct {
	id  uint32
	row itemMallSourceRow
}

type itemMallScrap struct {
	itemCode string
	optLevel uint8
	variance uint64
	data     uint32
	row      itemMallSourceRow
}

type itemMallPrice struct {
	currency uint8
	price    uint32
	row      itemMallSourceRow
}

func importItemMall(ctx context.Context, arguments []string, output io.Writer) (runErr error) {
	flags := flag.NewFlagSet("sro-data import-item-mall", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	mediaDirectory := flags.String("media", "", "path to the extracted Media directory")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *mediaDirectory == "" {
		return errItemMallImportUsage
	}

	placements, err := loadItemMallPlacements(filepath.Join(*mediaDirectory, "server_dep", "silkroad", "textdata"))
	if err != nil {
		return err
	}
	config.Initialize()
	pool, err := frameworkdb.OpenPool(ctx, frameworkdb.ShardPool, frameworkdb.ShardPoolConfig())
	if err != nil {
		return fmt.Errorf("open shard database: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, pool.Close()) }()

	goods, err := resolveItemMallItems(ctx, pool, placements)
	if err != nil {
		return err
	}
	if err := replaceItemMallGoods(ctx, pool, goods); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "imported %d item mall goods\n", len(goods))
	return err
}

func loadItemMallPlacements(directory string) ([]itemMallPlacement, error) {
	files := []struct {
		name string
		min  int
	}{
		{"refshopgroup.txt", 5},
		{"refmappingshopgroup.txt", 4},
		{"refmappingshopwithtab.txt", 4},
		{"refshoptab.txt", 5},
		{"refshopgoods.txt", 5},
		{"refpackageitem.txt", 4},
		{"refscrapofpackageitem.txt", 7},
		{"refpricepolicyofitem.txt", 14},
	}
	loaded := make(map[string][]itemMallSourceRow, len(files))
	for _, spec := range files {
		rows, err := loadItemMallFile(filepath.Join(directory, spec.name), spec.name, spec.min, spec.name == "refpricepolicyofitem.txt")
		if err != nil {
			return nil, err
		}
		loaded[spec.name] = rows
	}

	groups := make(map[string]uint16)
	groupIDs := make(map[uint16]string)
	for _, row := range loaded["refshopgroup.txt"] {
		id, err := itemMallUint(row, 2, 16, "shop group ID")
		if err != nil {
			return nil, err
		}
		code := row.fields[3]
		if code == "" || row.fields[4] == "" {
			return nil, itemMallRowError(row, "empty shop group code or NPC code")
		}
		if _, exists := groups[code]; exists {
			return nil, itemMallRowError(row, "duplicate shop group code %q", code)
		}
		if previous, exists := groupIDs[uint16(id)]; exists {
			return nil, itemMallRowError(row, "duplicate shop group ID %d (also %q)", id, previous)
		}
		groups[code] = uint16(id)
		groupIDs[uint16(id)] = code
	}

	groupMappings, err := itemMallMappings(loaded["refmappingshopgroup.txt"], "shop group mapping")
	if err != nil {
		return nil, err
	}
	shopTabMappings, err := itemMallMappings(loaded["refmappingshopwithtab.txt"], "shop tab mapping")
	if err != nil {
		return nil, err
	}
	shopsByTabGroup := make(map[string][]itemMallMapping)
	for _, mapping := range shopTabMappings {
		shopsByTabGroup[mapping.right] = append(shopsByTabGroup[mapping.right], mapping)
	}
	groupsByShop := make(map[string][]itemMallMapping)
	for _, mapping := range groupMappings {
		groupsByShop[mapping.right] = append(groupsByShop[mapping.right], mapping)
	}

	tabs := make(map[string]itemMallTab)
	tabCounts := make(map[string]uint16)
	for _, row := range loaded["refshoptab.txt"] {
		code, group := row.fields[3], row.fields[4]
		if code == "" || group == "" {
			return nil, itemMallRowError(row, "empty tab code or tab group")
		}
		if _, exists := tabs[code]; exists {
			return nil, itemMallRowError(row, "duplicate tab code %q", code)
		}
		index := tabCounts[group]
		if index > 255 {
			return nil, itemMallRowError(row, "tab index %d exceeds TINYINT UNSIGNED", index)
		}
		tabs[code] = itemMallTab{group: group, index: uint8(index), row: row}
		tabCounts[group]++
	}

	packages := make(map[string]itemMallPackage)
	packageIDs := make(map[uint32]string)
	for _, row := range loaded["refpackageitem.txt"] {
		id, err := itemMallUint(row, 2, 32, "package ID")
		if err != nil {
			return nil, err
		}
		code := row.fields[3]
		if code == "" || utf8.RuneCountInString(code) > 128 {
			return nil, itemMallRowError(row, "invalid package code length for %q", code)
		}
		if _, exists := packages[code]; exists {
			return nil, itemMallRowError(row, "duplicate package code %q", code)
		}
		if previous, exists := packageIDs[uint32(id)]; exists {
			return nil, itemMallRowError(row, "duplicate package ID %d (also %q)", id, previous)
		}
		packages[code] = itemMallPackage{id: uint32(id), row: row}
		packageIDs[uint32(id)] = code
	}

	scraps := make(map[string]itemMallScrap)
	for _, row := range loaded["refscrapofpackageitem.txt"] {
		packageCode := row.fields[2]
		if _, exists := scraps[packageCode]; exists {
			return nil, itemMallRowError(row, "duplicate scrap for package %q", packageCode)
		}
		optLevel, err := itemMallUint(row, 4, 8, "OptLevel")
		if err != nil {
			return nil, err
		}
		variance, err := itemMallUint(row, 5, 64, "Variance")
		if err != nil {
			return nil, err
		}
		data, err := itemMallUint(row, 6, 32, "Data")
		if err != nil {
			return nil, err
		}
		if row.fields[3] == "" {
			return nil, itemMallRowError(row, "empty scrap item code")
		}
		scraps[packageCode] = itemMallScrap{row.fields[3], uint8(optLevel), variance, uint32(data), row}
	}

	prices := make(map[string][]itemMallPrice)
	seenPrices := make(map[string]bool)
	for _, row := range loaded["refpricepolicyofitem.txt"] {
		device, err := itemMallUint(row, 3, 32, "payment device")
		if err != nil {
			return nil, err
		}
		key := row.fields[2] + "\x00" + strconv.FormatUint(device, 10)
		if seenPrices[key] {
			return nil, itemMallRowError(row, "duplicate price for package %q and payment device %d", row.fields[2], device)
		}
		seenPrices[key] = true
		currency, mapped := mapItemMallCurrency(device)
		if !mapped {
			continue
		}
		price, err := itemMallUint(row, 5, 32, "price")
		if err != nil {
			return nil, err
		}
		if price > 0 {
			prices[row.fields[2]] = append(prices[row.fields[2]], itemMallPrice{currency, uint32(price), row})
		}
	}

	var placements []itemMallPlacement
	positions := make(map[[4]uint16]string)
	for _, row := range loaded["refshopgoods.txt"] {
		tabCode := row.fields[2]
		if !strings.HasPrefix(tabCode, "MALL_") {
			continue
		}
		tab, exists := tabs[tabCode]
		if !exists {
			return nil, itemMallRowError(row, "unresolved tab %q", tabCode)
		}
		shopMappings := shopsByTabGroup[tab.group]
		if len(shopMappings) != 1 {
			return nil, itemMallRowError(row, "tab group %q resolves to %d shops, want 1", tab.group, len(shopMappings))
		}
		groupLinks := groupsByShop[shopMappings[0].left]
		if len(groupLinks) != 1 {
			return nil, itemMallRowError(row, "shop %q resolves to %d shop groups, want 1", shopMappings[0].left, len(groupLinks))
		}
		groupID, exists := groups[groupLinks[0].left]
		if !exists {
			return nil, itemMallRowError(row, "unresolved shop group %q", groupLinks[0].left)
		}
		slot, err := itemMallUint(row, 4, 8, "slot index")
		if err != nil {
			return nil, err
		}
		packageCode := row.fields[3]
		pkg, exists := packages[packageCode]
		if !exists {
			return nil, itemMallRowError(row, "unresolved package %q", packageCode)
		}
		scrap, exists := scraps[packageCode]
		if !exists {
			return nil, itemMallRowError(row, "unresolved scrap for package %q", packageCode)
		}
		packagePrices := prices[packageCode]
		if len(packagePrices) == 0 {
			continue
		}
		if len(packagePrices) > 1 {
			return nil, itemMallRowError(row, "package %q has %d positive mapped prices", packageCode, len(packagePrices))
		}
		position := [4]uint16{groupID, uint16(groupLinks[0].index), uint16(tab.index), uint16(slot)}
		if previous, exists := positions[position]; exists {
			return nil, itemMallRowError(row, "duplicate target position (also package %q)", previous)
		}
		positions[position] = packageCode
		placements = append(placements, itemMallPlacement{shopGroupID: groupID, groupIndex: groupLinks[0].index, tabIndex: tab.index, slotIndex: uint8(slot), packageID: pkg.id, packageCode: packageCode, itemCode: scrap.itemCode, currency: packagePrices[0].currency, price: packagePrices[0].price, quantity: 1, optLevel: scrap.optLevel, variance: scrap.variance, data: scrap.data})
	}
	if len(placements) == 0 {
		return nil, errors.New("item mall catalog contains no enabled country 15 placements with positive mapped prices")
	}
	return placements, nil
}

func loadItemMallFile(path, name string, minimumFields int, exact bool) ([]itemMallSourceRow, error) {
	contents, err := readClientTextFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	var rows []itemMallSourceRow
	scanner := bufio.NewScanner(strings.NewReader(contents))
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSuffix(scanner.Text(), "\r")
		if text == "" {
			continue
		}
		fields := strings.Split(text, "\t")
		if (exact && len(fields) != minimumFields) || (!exact && len(fields) < minimumFields) {
			return nil, fmt.Errorf("%s line %d: want %s %d fields, got %d", name, line, map[bool]string{true: "exactly", false: "at least"}[exact], minimumFields, len(fields))
		}
		row := itemMallSourceRow{name, line, fields}
		service, err := itemMallUint(row, 0, 8, "Service")
		if err != nil {
			return nil, err
		}
		country, err := itemMallUint(row, 1, 8, "Country")
		if err != nil {
			return nil, err
		}
		if service == 1 && country == 15 {
			rows = append(rows, row)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	return rows, nil
}

func itemMallMappings(rows []itemMallSourceRow, kind string) ([]itemMallMapping, error) {
	counts := make(map[string]uint16)
	seen := make(map[string]bool)
	mappings := make([]itemMallMapping, 0, len(rows))
	for _, row := range rows {
		left, right := row.fields[2], row.fields[3]
		if left == "" || right == "" {
			return nil, itemMallRowError(row, "empty %s endpoint", kind)
		}
		key := left + "\x00" + right
		if seen[key] {
			return nil, itemMallRowError(row, "duplicate %s %q -> %q", kind, left, right)
		}
		index := counts[left]
		if index > 255 {
			return nil, itemMallRowError(row, "%s index %d exceeds TINYINT UNSIGNED", kind, index)
		}
		seen[key] = true
		mappings = append(mappings, itemMallMapping{left, right, uint8(index), row})
		counts[left]++
	}
	return mappings, nil
}

func itemMallUint(row itemMallSourceRow, index, bits int, name string) (uint64, error) {
	value, err := strconv.ParseUint(strings.TrimSpace(row.fields[index]), 10, bits)
	if err != nil {
		return 0, itemMallRowError(row, "%s field %d: %v", name, index, err)
	}
	return value, nil
}

func itemMallRowError(row itemMallSourceRow, format string, arguments ...any) error {
	return fmt.Errorf("%s line %d: %s", row.file, row.line, fmt.Sprintf(format, arguments...))
}

func mapItemMallCurrency(device uint64) (uint8, bool) {
	switch device {
	case 2:
		return 0, true
	case 4:
		return 1, true
	case 16:
		return 2, true
	default:
		return 0, false
	}
}

func resolveItemMallItems(ctx context.Context, pool *sql.DB, placements []itemMallPlacement) ([]itemMallGood, error) {
	rows, err := pool.QueryContext(ctx, "SELECT `ItemID`,`CodeName`,COALESCE(NULLIF(`Stack_Size`,0),1) FROM `ITEMDATA` WHERE `CodeName` IS NOT NULL")
	if err != nil {
		return nil, fmt.Errorf("query ITEMDATA: %w", err)
	}
	defer rows.Close()
	type itemReference struct {
		id       int32
		maxStack uint32
	}
	items := make(map[string]itemReference)
	for rows.Next() {
		var id int64
		var code string
		var maxStack uint32
		if err := rows.Scan(&id, &code, &maxStack); err != nil {
			return nil, fmt.Errorf("scan ITEMDATA: %w", err)
		}
		if id < -2147483648 || id > 2147483647 {
			return nil, fmt.Errorf("ITEMDATA item %q ID %d exceeds signed INT", code, id)
		}
		if _, exists := items[code]; exists {
			return nil, fmt.Errorf("ITEMDATA contains duplicate CodeName %q", code)
		}
		items[code] = itemReference{id: int32(id), maxStack: maxStack}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read ITEMDATA: %w", err)
	}
	goods := make([]itemMallGood, 0, len(placements))
	for _, placement := range placements {
		item, exists := items[placement.itemCode]
		if !exists {
			return nil, fmt.Errorf("package %q: ITEMDATA does not contain CodeName %q", placement.packageCode, placement.itemCode)
		}
		if err := placement.applyStackablePackageQuantity(item.maxStack); err != nil {
			return nil, err
		}
		goods = append(goods, itemMallGood{placement, item.id})
	}
	return goods, nil
}

func (placement *itemMallPlacement) applyStackablePackageQuantity(maxStack uint32) error {
	if maxStack <= 1 || placement.data == 0 {
		return nil
	}
	if placement.data > math.MaxUint16 {
		return fmt.Errorf("package %q: stack quantity %d exceeds SMALLINT UNSIGNED", placement.packageCode, placement.data)
	}
	placement.quantity = uint16(placement.data)
	placement.data = 0
	return nil
}

func replaceItemMallGoods(ctx context.Context, pool *sql.DB, goods []itemMallGood) error {
	if len(goods) == 0 {
		return errors.New("refusing to replace item mall with an empty catalog")
	}
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin item mall import: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM `ITEM_MALL_GOODS`"); err != nil {
		return fmt.Errorf("clear ITEM_MALL_GOODS: %w", err)
	}
	statement, err := tx.PrepareContext(ctx, "INSERT INTO `ITEM_MALL_GOODS` (`REF_SHOP_GROUP_ID`,`GROUP_INDEX`,`TAB_INDEX`,`SLOT_INDEX`,`PACKAGE_ID`,`PACKAGE_CODE_NAME`,`FK_REF_ITEM`,`CURRENCY`,`PRICE`,`QUANTITY`,`OPT_LEVEL`,`VARIANCE`,`DATA`,`ENABLED`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,1)")
	if err != nil {
		return fmt.Errorf("prepare item mall import: %w", err)
	}
	defer statement.Close()
	for _, good := range goods {
		if _, err := statement.ExecContext(ctx, good.shopGroupID, good.groupIndex, good.tabIndex, good.slotIndex, good.packageID, good.packageCode, good.itemID, good.currency, good.price, good.quantity, good.optLevel, good.variance, good.data); err != nil {
			return fmt.Errorf("insert item mall package %q: %w", good.packageCode, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit item mall import: %w", err)
	}
	return nil
}
