package db

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"fmt"
)

//go:embed schema.sql
var postgresSchema string

//go:embed schema.sqlite
var sqliteSchema string

//go:embed schema_v2.sql
var postgresSchemaV2 string

//go:embed schema_v2.sqlite
var sqliteSchemaV2 string

//go:embed schema_v3.sql
var postgresSchemaV3 string

//go:embed schema_v3.sqlite
var sqliteSchemaV3 string

//go:embed schema_v4.sql
var postgresSchemaV4 string

//go:embed schema_v4.sqlite
var sqliteSchemaV4 string

//go:embed schema_v5.sql
var postgresSchemaV5 string

//go:embed schema_v5.sqlite
var sqliteSchemaV5 string

type migration struct {
	version int
	body    string
}

// Migrate applies pending migrations in a transaction. A checksum protects
// already-applied versions from accidental edits. Append a new version to evolve
// a deployed schema; never edit an applied migration.
func (db *DB) Migrate(ctx context.Context) error {
	var bodyV1, bodyV2, bodyV3, bodyV4, bodyV5 string
	switch db.Dialect {
	case Postgres:
		bodyV1 = postgresSchema
		bodyV2 = postgresSchemaV2
		bodyV3 = postgresSchemaV3
		bodyV4 = postgresSchemaV4
		bodyV5 = postgresSchemaV5
	case SQLite:
		bodyV1 = sqliteSchema
		bodyV2 = sqliteSchemaV2
		bodyV3 = sqliteSchemaV3
		bodyV4 = sqliteSchemaV4
		bodyV5 = sqliteSchemaV5
	default:
		return fmt.Errorf("unsupported migration dialect %q", db.Dialect)
	}
	return db.migrate(ctx, []migration{
		{version: 1, body: bodyV1},
		{version: 2, body: bodyV2},
		{version: 3, body: bodyV3},
		{version: 4, body: bodyV4},
		{version: 5, body: bodyV5},
	})
}

func (db *DB) migrate(ctx context.Context, migrations []migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration: %w", err)
	}
	defer tx.Rollback()
	if db.Dialect == Postgres {
		// A transaction-scoped advisory lock serializes independent server starts.
		if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(706598199001)"); err != nil {
			return fmt.Errorf("lock migrations: %w", err)
		}
	}
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		checksum VARCHAR(64) NOT NULL,
		applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	// Reject unknown versions rather than letting an older binary operate on an
	// incompatible newer schema.
	var latest int
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&latest); err != nil {
		return fmt.Errorf("read migration version: %w", err)
	}
	last := 0
	for _, m := range migrations {
		if m.version <= last || m.body == "" {
			return fmt.Errorf("invalid migration sequence at version %d", m.version)
		}
		last = m.version
	}
	if latest > last {
		return fmt.Errorf("database schema version %d is newer than supported version %d", latest, last)
	}
	for _, m := range migrations {
		expected := fmt.Sprintf("%x", sha256.Sum256([]byte(m.body)))
		var checksum string
		err = tx.QueryRowContext(ctx, "SELECT checksum FROM schema_migrations WHERE version = $1", m.version).Scan(&checksum)
		if err == nil {
			if checksum != expected {
				return fmt.Errorf("migration %d checksum mismatch", m.version)
			}
			continue
		}
		if err != sql.ErrNoRows {
			return fmt.Errorf("read migration %d: %w", m.version, err)
		}
		if _, err = tx.ExecContext(ctx, m.body); err != nil {
			return fmt.Errorf("apply migration %d: %w", m.version, err)
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations (version, checksum) VALUES ($1, $2)", m.version, expected); err != nil {
			return fmt.Errorf("record migration %d: %w", m.version, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	return nil
}
