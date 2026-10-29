// Package db opens pooled PostgreSQL and SQLite databases and applies embedded,
// versioned migrations before returning a usable connection.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/mattn/go-sqlite3"
)

type Dialect string

const (
	Postgres Dialect = "postgres"
	SQLite   Dialect = "sqlite"
)

// Config configures the database/sql pool. Zero values select safe defaults.
// SQLite requires CGO and a C compiler. DSN is a PostgreSQL URL/connection string
// or a SQLite filename/URI; an empty SQLite DSN selects an in-memory database.
// PostgreSQL DSNs should explicitly configure sslmode for the deployment.
// Schema specifies an isolated PostgreSQL schema (e.g. "meuseai") for shared
// databases like Supabase slots.
type Config struct {
	Driver          string
	DSN             string
	Schema          string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// DB embeds the standard SQL API. Queries using $1, $2, ... bind parameters work
// in both supported databases. Call Close when the owning server shuts down.
type DB struct {
	*sql.DB
	Dialect Dialect
}

// Open pings the database and applies migrations atomically. On failure it closes
// the pool. Context controls both connection establishment and migrations.
func Open(ctx context.Context, cfg Config) (*DB, error) {
	if cfg.MaxOpenConns < 0 || cfg.MaxIdleConns < 0 || cfg.ConnMaxLifetime < 0 {
		return nil, errors.New("database pool settings cannot be negative")
	}
	var driver string
	var dialect Dialect
	switch cfg.Driver {
	case "postgres", "postgresql":
		driver, dialect = "postgres", Postgres
		if strings.TrimSpace(cfg.DSN) == "" {
			return nil, errors.New("postgres requires a DSN")
		}
		if cfg.Schema != "" {
			if !validIdentifier(cfg.Schema) {
				return nil, fmt.Errorf("invalid schema name %q", cfg.Schema)
			}
			cfg.DSN = withPostgresSearchPath(cfg.DSN, cfg.Schema)
		}
	case "sqlite", "sqlite3":
		driver, dialect = "sqlite3", SQLite
		var err error
		cfg.DSN, err = sqliteDSN(cfg.DSN)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported database driver %q", cfg.Driver)
	}
	maxOpen, maxIdle := cfg.MaxOpenConns, cfg.MaxIdleConns
	if maxOpen == 0 {
		maxOpen = 25
		if dialect == SQLite {
			maxOpen = 1
		}
	}
	// A memory database belongs to its connection; keep exactly one alive, even
	// when callers request more connections or connection expiry.
	memory := dialect == SQLite && isMemoryDSN(cfg.DSN)
	if memory {
		maxOpen = 1
	}
	if maxIdle == 0 {
		maxIdle = 5
	}
	if maxIdle > maxOpen {
		maxIdle = maxOpen
	}
	lifetime := cfg.ConnMaxLifetime
	if lifetime == 0 {
		lifetime = 30 * time.Minute
	}
	if memory {
		lifetime = 0
	}
	pool, err := sql.Open(driver, cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("open %s database: %w", dialect, err)
	}
	pool.SetMaxOpenConns(maxOpen)
	pool.SetMaxIdleConns(maxIdle)
	pool.SetConnMaxLifetime(lifetime)
	database := &DB{DB: pool, Dialect: dialect}
	if err = pingDatabase(ctx, pool, dialect); err == nil {
		if cfg.Schema != "" && dialect == Postgres {
			_, err = pool.ExecContext(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", cfg.Schema))
		}
		if err == nil {
			err = database.Migrate(ctx)
		}
	}
	if err != nil {
		pool.Close()
		// Driver errors may contain credentials from the DSN; do not add it here.
		return nil, fmt.Errorf("initialize %s database: %w", dialect, err)
	}
	return database, nil
}

func sqliteDSN(dsn string) (string, error) {
	if dsn == "" {
		dsn = ":memory:"
	}
	base, rawQuery, _ := strings.Cut(dsn, "?")
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "", errors.New("invalid SQLite DSN query parameters")
	}
	// Remove aliases that would otherwise override our required settings.
	query.Del("_fk")
	query.Del("_timeout")
	query.Del("_journal")
	// URI parameters apply to every pooled connection, unlike a one-time PRAGMA.
	query.Set("_foreign_keys", "on")
	query.Set("_busy_timeout", "5000")
	query.Set("_journal_mode", "WAL")
	// BEGIN IMMEDIATE serializes migration writers before reading their version.
	query.Set("_txlock", "immediate")
	return base + "?" + query.Encode(), nil
}

func isMemoryDSN(dsn string) bool {
	base, query, _ := strings.Cut(dsn, "?")
	values, _ := url.ParseQuery(query)
	return base == ":memory:" || base == "file::memory:" || values.Get("mode") == "memory"
}

func validIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func withPostgresSearchPath(dsn, schema string) string {
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		if u, err := url.Parse(dsn); err == nil {
			q := u.Query()
			if q.Get("search_path") == "" {
				q.Set("search_path", schema)
				u.RawQuery = q.Encode()
				return u.String()
			}
		}
		return dsn
	}
	if !strings.Contains(dsn, "search_path=") {
		return strings.TrimSpace(dsn) + " search_path=" + schema
	}
	return dsn
}

// Concurrent SQLite opens can briefly collide while the driver enables WAL,
// before its busy timeout takes effect. Retry only that transient lock error.
func pingDatabase(ctx context.Context, pool *sql.DB, dialect Dialect) error {
	var err error
	for attempt := 0; attempt < 50; attempt++ {
		err = pool.PingContext(ctx)
		var sqliteErr sqlite3.Error
		if err == nil || dialect != SQLite || !errors.As(err, &sqliteErr) || (sqliteErr.Code != sqlite3.ErrBusy && sqliteErr.Code != sqlite3.ErrLocked) {
			return err
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return err
}
