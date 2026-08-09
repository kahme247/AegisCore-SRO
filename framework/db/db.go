package db

import (
	"database/sql"
	"github.com/spf13/viper"

	"github.com/Harvi8/AegisCore/framework/config"

	_ "github.com/go-sql-driver/mysql"
)

// OpenConnAccount is retained for legacy callers that still own a short-lived
// handle. New startup code should use NewConfiguredPools and inject the
// resulting pool into repositories instead.
func OpenConnAccount() (db *sql.DB) {
	db, errDb := sql.Open(viper.GetString(config.DatabaseAccountDriver), config.ConnStringAccount())
	if errDb != nil {
		panic(errDb.Error())
	}
	return db
}

// OpenConnShard is retained for legacy callers that still own a short-lived
// handle. New startup code should use NewConfiguredPools and inject the
// resulting pool into repositories instead.
func OpenConnShard() (db *sql.DB) {
	db, errDb := sql.Open(viper.GetString(config.DatabaseShardDriver), config.ConnStringShard())
	if errDb != nil {
		panic(errDb.Error())
	}
	return db
}

// CheckError is retained only for unmigrated legacy code. New repositories
// must return errors to their callers instead of invoking this panic helper.
func CheckError(err error) {
	if err != nil {
		panic(err)
	}
}
