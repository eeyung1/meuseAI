# Database foundation

Requires Go 1.24+, CGO, and a C compiler (SQLite uses `go-sqlite3`). HTTP routing
can use Go's standard `net/http.ServeMux`; this module adds only SQL drivers.

```go
ctx := context.Background()
database, err := db.Open(ctx, db.Config{
    Driver: "sqlite", // or "postgres"
    DSN:    "file:meuseai.db", // PostgreSQL: use your deployment connection string
})
if err != nil {
    return err
}
defer database.Close()
```

`DB` embeds `*sql.DB`. Use `ExecContext`, `QueryContext`, `BeginTx`, and `$1`,
`$2`, etc. placeholders for either dialect. `DB.Dialect` identifies the backend.
Transactions for payment updates and progress advancement belong to their callers.
`Open` pings the database, then runs embedded migrations before returning it.

The pool defaults to 25 open / 5 idle connections for PostgreSQL, and 1 open /
1 idle for SQLite. Configure `MaxOpenConns`, `MaxIdleConns`, and
`ConnMaxLifetime` to override defaults. Memory databases always retain one
connection without lifetime expiry so the schema survives between queries.
SQLite connections enable foreign keys, WAL, a 5-second busy timeout, and
immediate write transactions; reserved DSN options and aliases are overridden.
File-backed SQLite can use a larger pool, but only one writer runs at a time.

`schema.sql` and `schema.sqlite` are the embedded version-1 PostgreSQL and
SQLite migrations. Migration metadata stores a version, SHA-256 checksum, and
application timestamp. Each run holds a transaction-scoped PostgreSQL advisory
lock or SQLite immediate transaction, checks applied versions, and atomically
applies pending migrations. A changed checksum or newer schema fails startup.
Append new migration versions in `migrate.go`; never modify deployed versions.

The schema includes consent, verification and unsubscribe tokens, delivery
suppression (`unsubscribed_at`), student progress, integer-kobo payments, and
activity logs. `updated_at` and payment `paid_at` must be set by the service when
the corresponding event occurs. Payment references, email addresses (including
case variants), unsubscribe tokens, and per-student daily progress are unique.
A student with payments cannot be deleted until those references are addressed.

Run local tests:

```sh
go test -v ./db/...
go test -race ./db/...
go vet ./...
```

The shared behavioral suite covers schema creation, CRUD, constraints, rollback,
and idempotent migrations on SQLite and, when configured, PostgreSQL. SQLite
also tests reconnect persistence, pooled foreign keys, concurrent startup,
checksum rejection, future-schema rejection, and migration rollback.

Set `MEUSEAI_TEST_POSTGRES_DSN` to run PostgreSQL integration tests. The database
user must be able to create and drop a test schema. Tests create a unique schema
and remove only that schema. Without this setting, PostgreSQL is explicitly
skipped; a passing SQLite run is not PostgreSQL runtime evidence.
