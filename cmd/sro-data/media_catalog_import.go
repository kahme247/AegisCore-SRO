package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Harvi8/AegisCore/framework/config"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

var errMediaCatalogImportUsage = errors.New("usage: sro-data import-media-catalogs --media <Media directory>")

type mediaCatalogImportFile struct {
	Name string
	Rows [][]string
}

func importMediaCatalogs(ctx context.Context, arguments []string, output io.Writer) (runErr error) {
	flags := flag.NewFlagSet("sro-data import-media-catalogs", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	mediaDirectory := flags.String("media", "", "path to the extracted Media directory")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || strings.TrimSpace(*mediaDirectory) == "" {
		return errMediaCatalogImportUsage
	}
	files, err := loadMediaCatalogImportFiles(filepath.Join(*mediaDirectory, filepath.FromSlash(mediaTextDataPath)))
	if err != nil {
		return err
	}
	config.Initialize()
	pool, err := frameworkdb.OpenPool(ctx, frameworkdb.ShardPool, frameworkdb.ShardPoolConfig())
	if err != nil {
		return fmt.Errorf("open shard database: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, pool.Close()) }()
	if err := replaceMediaCatalogRows(ctx, pool, files); err != nil {
		return err
	}
	rowCount := 0
	for _, file := range files {
		rowCount += len(file.Rows)
	}
	_, err = fmt.Fprintf(output, "imported %d Media catalogs with %d rows\n", len(files), rowCount)
	return err
}

func loadMediaCatalogImportFiles(textDataDirectory string) ([]mediaCatalogImportFile, error) {
	entries, err := os.ReadDir(textDataDirectory)
	if err != nil {
		return nil, fmt.Errorf("read Media textdata directory: %w", err)
	}
	var result []mediaCatalogImportFile
	for _, entry := range entries {
		if entry.IsDir() || !isPassthroughMediaCatalog(entry.Name()) {
			continue
		}
		contents, err := readClientTextFile(filepath.Join(textDataDirectory, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read Media catalog %s: %w", entry.Name(), err)
		}
		lines := strings.Split(strings.ReplaceAll(contents, "\r\n", "\n"), "\n")
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		rows := make([][]string, 0, len(lines))
		for lineNumber, line := range lines {
			if line == "" {
				continue
			}
			fields := strings.Split(strings.TrimSuffix(line, "\r"), "\t")
			for _, field := range fields {
				if strings.ContainsAny(field, "\r\n") {
					return nil, fmt.Errorf("Media catalog %s line %d contains a newline inside a field", entry.Name(), lineNumber+1)
				}
			}
			rows = append(rows, fields)
		}
		result = append(result, mediaCatalogImportFile{Name: strings.ToLower(entry.Name()), Rows: rows})
	}
	if len(result) == 0 {
		return nil, errors.New("Media directory contains no supported passthrough catalogs")
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func replaceMediaCatalogRows(ctx context.Context, pool *sql.DB, files []mediaCatalogImportFile) error {
	tx, err := pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin Media catalog import: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM SRO_SHARD.REF_MEDIA_TEXT_CATALOG"); err != nil {
		return fmt.Errorf("clear Media text catalogs: %w", err)
	}
	fileStatement, err := tx.PrepareContext(ctx, "INSERT INTO SRO_SHARD.REF_MEDIA_TEXT_CATALOG (FILE_NAME) VALUES (?)")
	if err != nil {
		return err
	}
	defer fileStatement.Close()
	rowStatement, err := tx.PrepareContext(ctx, "INSERT INTO SRO_SHARD.REF_MEDIA_TEXT_CATALOG_ROW (FILE_NAME,ROW_ORDINAL,RAW_FIELDS) VALUES (?,?,CAST(? AS JSON))")
	if err != nil {
		return err
	}
	defer rowStatement.Close()
	for _, file := range files {
		if !isPassthroughMediaCatalog(file.Name) {
			return fmt.Errorf("Media catalog %q is not approved for passthrough storage", file.Name)
		}
		if _, err := fileStatement.ExecContext(ctx, file.Name); err != nil {
			return fmt.Errorf("insert Media catalog %s: %w", file.Name, err)
		}
		for index, fields := range file.Rows {
			raw, err := json.Marshal(fields)
			if err != nil {
				return err
			}
			if _, err := rowStatement.ExecContext(ctx, file.Name, index, string(raw)); err != nil {
				return fmt.Errorf("insert Media catalog %s row %d: %w", file.Name, index+1, err)
			}
		}
	}
	return tx.Commit()
}

func isPassthroughMediaCatalog(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, allowed := range unsupportedMediaCatalogs {
		if allowed == name {
			return true
		}
	}
	return false
}
