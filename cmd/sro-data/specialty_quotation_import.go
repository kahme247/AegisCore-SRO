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
	"os"
	"strconv"
	"strings"

	"github.com/Harvi8/AegisCore/framework/config"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

var errSpecialtyQuotationImportUsage = errors.New("usage: sro-data import-specialty-quotations --source <pipe-delimited _ItemQuotation export>")

type specialtyQuotationRow struct {
	ID              int32
	Service         int32
	AssocNPC        int32
	RefItemID       uint32
	BaseQuot        float64
	QuotLB          float64
	QuotUB          float64
	BaseStockAmount int32
	FluctuateAmount int32
	CurStockAmount  int32
}

func importSpecialtyQuotations(ctx context.Context, arguments []string, output io.Writer) (runErr error) {
	flags := flag.NewFlagSet("sro-data import-specialty-quotations", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	source := flags.String("source", "", "pipe-delimited export of dbo._ItemQuotation")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || strings.TrimSpace(*source) == "" {
		return errSpecialtyQuotationImportUsage
	}
	rows, err := loadSpecialtyQuotationRows(*source)
	if err != nil {
		return err
	}
	config.Initialize()
	pool, err := frameworkdb.OpenPool(ctx, frameworkdb.ShardPool, frameworkdb.ShardPoolConfig())
	if err != nil {
		return fmt.Errorf("open shard database: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, pool.Close()) }()
	if err := replaceSpecialtyQuotations(ctx, pool, rows); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "imported %d specialty quotation rows\n", len(rows))
	return err
}

func loadSpecialtyQuotationRows(path string) ([]specialtyQuotationRow, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open specialty quotation source: %w", err)
	}
	defer file.Close()
	rows := make([]specialtyQuotationRow, 0, 512)
	ids := make(map[int32]struct{})
	pairs := make(map[[2]int64]struct{})
	scanner := bufio.NewScanner(file)
	line := 0
	for scanner.Scan() {
		line++
		value := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if value == "" {
			continue
		}
		fields := strings.Split(value, "|")
		if len(fields) != 10 {
			return nil, fmt.Errorf("specialty quotation line %d has %d fields, want 10", line, len(fields))
		}
		row, parseErr := parseSpecialtyQuotationRow(fields)
		if parseErr != nil {
			return nil, fmt.Errorf("specialty quotation line %d: %w", line, parseErr)
		}
		if _, exists := ids[row.ID]; exists {
			return nil, fmt.Errorf("specialty quotation line %d duplicates ID %d", line, row.ID)
		}
		pair := [2]int64{int64(row.AssocNPC), int64(row.RefItemID)}
		if _, exists := pairs[pair]; exists {
			return nil, fmt.Errorf("specialty quotation line %d duplicates NPC/item %d/%d", line, row.AssocNPC, row.RefItemID)
		}
		ids[row.ID], pairs[pair] = struct{}{}, struct{}{}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read specialty quotation source: %w", err)
	}
	if len(rows) == 0 {
		return nil, errors.New("specialty quotation source is empty")
	}
	return rows, nil
}

func parseSpecialtyQuotationRow(fields []string) (specialtyQuotationRow, error) {
	parseInt := func(index, bits int) (int64, error) {
		return strconv.ParseInt(strings.TrimSpace(fields[index]), 10, bits)
	}
	id, err := parseInt(0, 32)
	if err != nil || id <= 0 {
		return specialtyQuotationRow{}, errors.New("ID must be a positive int32")
	}
	service, err := parseInt(1, 32)
	if err != nil || (service != 0 && service != 1) {
		return specialtyQuotationRow{}, errors.New("Service must be 0 or 1")
	}
	npc, err := parseInt(2, 32)
	if err != nil || npc <= 0 {
		return specialtyQuotationRow{}, errors.New("AssocNPC must be a positive int32")
	}
	item, err := strconv.ParseUint(strings.TrimSpace(fields[3]), 10, 32)
	if err != nil || item == 0 {
		return specialtyQuotationRow{}, errors.New("RefItemID must be a positive uint32")
	}
	quotations := [3]float64{}
	for index := range quotations {
		quotations[index], err = strconv.ParseFloat(strings.TrimSpace(fields[4+index]), 32)
		if err != nil || math.IsNaN(quotations[index]) || math.IsInf(quotations[index], 0) {
			return specialtyQuotationRow{}, fmt.Errorf("quotation field %d is invalid", 4+index)
		}
	}
	if quotations[1] > quotations[0] || quotations[0] > quotations[2] {
		return specialtyQuotationRow{}, errors.New("quotation bounds must satisfy lower <= base <= upper")
	}
	stock := [3]int64{}
	for index := range stock {
		stock[index], err = parseInt(7+index, 32)
		if err != nil {
			return specialtyQuotationRow{}, fmt.Errorf("stock field %d is invalid", 7+index)
		}
	}
	if stock[0] <= 0 || stock[1] <= 0 || stock[2] < 0 {
		return specialtyQuotationRow{}, errors.New("stock values require positive base/fluctuation and non-negative current")
	}
	return specialtyQuotationRow{ID: int32(id), Service: int32(service), AssocNPC: int32(npc), RefItemID: uint32(item), BaseQuot: quotations[0], QuotLB: quotations[1], QuotUB: quotations[2], BaseStockAmount: int32(stock[0]), FluctuateAmount: int32(stock[1]), CurStockAmount: int32(stock[2])}, nil
}

func replaceSpecialtyQuotations(ctx context.Context, database *sql.DB, rows []specialtyQuotationRow) error {
	if database == nil || len(rows) == 0 {
		return errors.New("specialty quotation import requires a database and rows")
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM `ITEM_QUOTATION`"); err != nil {
		return fmt.Errorf("clear ITEM_QUOTATION: %w", err)
	}
	statement, err := tx.PrepareContext(ctx, "INSERT INTO `ITEM_QUOTATION` (`ID`,`SERVICE`,`ASSOC_NPC`,`REF_ITEM_ID`,`BASE_QUOT`,`QUOT_LB`,`QUOT_UB`,`BASE_STOCK_AMOUNT`,`FLUCTUATE_AMOUNT`,`CUR_STOCK_AMOUNT`) VALUES (?,?,?,?,?,?,?,?,?,?)")
	if err != nil {
		return err
	}
	defer statement.Close()
	for _, row := range rows {
		if _, err := statement.ExecContext(ctx, row.ID, row.Service, row.AssocNPC, row.RefItemID, row.BaseQuot, row.QuotLB, row.QuotUB, row.BaseStockAmount, row.FluctuateAmount, row.CurStockAmount); err != nil {
			return fmt.Errorf("insert ITEM_QUOTATION ID %d: %w", row.ID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit ITEM_QUOTATION import: %w", err)
	}
	return nil
}
