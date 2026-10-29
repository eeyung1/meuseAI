package db

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func openSQLite(t *testing.T, dsn string) *DB {
	t.Helper()
	database, err := Open(context.Background(), Config{Driver: "sqlite", DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func execSQL(t *testing.T, db *DB, query string, args ...any) sql.Result {
	t.Helper()
	result, err := db.ExecContext(context.Background(), query, args...)
	if err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return result
}

func insertStudent(t *testing.T, db *DB, id, email string) {
	t.Helper()
	execSQL(t, db, `INSERT INTO students (id, full_name, email, consent_timestamp, unsubscribe_token, verification_token)
		VALUES ($1, $2, $3, $4, $5, $6)`, id, "Ada Learner", email, time.Now().UTC(), "unsub-"+id, "verify-"+id)
}

func TestSQLiteSchemaAndCRUD(t *testing.T) {
	testSchemaAndCRUD(t, openSQLite(t, ":memory:"))
}

// The same behavioral suite runs against PostgreSQL when a test DSN is supplied.
// It creates an isolated schema and never drops preexisting tables.
func TestPostgresSchemaAndCRUD(t *testing.T) {
	dsn := os.Getenv("MEUSEAI_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("MEUSEAI_TEST_POSTGRES_DSN is not configured; PostgreSQL integration not run")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("meuseai_test_%d", time.Now().UnixNano())
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(cleanup, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Errorf("clean PostgreSQL test schema: %v", err)
		}
	}()
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		dsn = u.String()
	} else {
		dsn += " search_path=" + schema
	}
	database, err := Open(ctx, Config{Driver: "postgres", DSN: dsn})
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	testSchemaAndCRUD(t, database)
}

func testSchemaAndCRUD(t *testing.T, db *DB) {
	t.Helper()
	ctx := context.Background()
	insertStudent(t, db, "ada", "ada@example.test")
	var verified, ready bool
	var xp, streak int
	var stage, status, tier string
	var consent time.Time
	if err := db.QueryRowContext(ctx, `SELECT email_verified, tier_2_ready, xp, streak_days,
		current_stage, engagement_status, funnel_tier, consent_timestamp FROM students WHERE id=$1`, "ada").Scan(
		&verified, &ready, &xp, &streak, &stage, &status, &tier, &consent); err != nil {
		t.Fatal(err)
	}
	if verified || ready || xp != 0 || streak != 1 || stage != "day_1" || status != "pending" || tier != "free_week" || consent.IsZero() {
		t.Fatalf("incorrect defaults: verified=%v ready=%v xp=%d streak=%d stage=%s status=%s tier=%s consent=%v", verified, ready, xp, streak, stage, status, tier, consent)
	}
	execSQL(t, db, "UPDATE students SET email_verified=$1, xp=$2 WHERE id=$3", true, 250, "ada")
	if err := db.QueryRowContext(ctx, "SELECT xp, email_verified FROM students WHERE id=$1", "ada").Scan(&xp, &verified); err != nil || xp != 250 || !verified {
		t.Fatalf("student update: xp=%d verified=%v err=%v", xp, verified, err)
	}
	execSQL(t, db, "INSERT INTO student_progress (student_id,day_number,submission_payload) VALUES ($1,$2,$3)", "ada", 1, "Subject + Style + Setting")
	execSQL(t, db, "UPDATE student_progress SET completed=$1, completed_at=$2 WHERE student_id=$3 AND day_number=$4", true, time.Now().UTC(), "ada", 1)
	var completed bool
	var payload string
	if err := db.QueryRowContext(ctx, "SELECT completed, submission_payload FROM student_progress WHERE student_id=$1 AND day_number=$2", "ada", 1).Scan(&completed, &payload); err != nil || !completed || payload == "" {
		t.Fatalf("progress update: completed=%v payload=%q err=%v", completed, payload, err)
	}
	execSQL(t, db, `INSERT INTO payments (reference,student_id,amount_kobo,tier_unlocked,status,raw_payload)
		VALUES ($1,$2,$3,$4,$5,$6)`, "ref-1", "ada", 500000, "tier_1", "pending", `{"event":"charge.success"}`)
	execSQL(t, db, "UPDATE payments SET status=$1,paid_at=$2 WHERE reference=$3", "success", time.Now().UTC(), "ref-1")
	var amount int64
	var currency string
	if err := db.QueryRowContext(ctx, "SELECT amount_kobo,currency,status FROM payments WHERE reference=$1", "ref-1").Scan(&amount, &currency, &status); err != nil || amount != 500000 || currency != "NGN" || status != "success" {
		t.Fatalf("payment CRUD: amount=%d currency=%s status=%s err=%v", amount, currency, status, err)
	}
	execSQL(t, db, "INSERT INTO activity_logs (student_id,event_type,details) VALUES ($1,$2,$3)", "ada", "payment_success", `{"tier":"tier_1"}`)
	if err := db.QueryRowContext(ctx, "SELECT event_type FROM activity_logs WHERE student_id=$1", "ada").Scan(&status); err != nil || status != "payment_success" {
		t.Fatalf("activity CRUD: status=%s err=%v", status, err)
	}
	// Duplicate payment references and per-day completions must be rejected.
	for name, query := range map[string]string{
		"payment reference":      `INSERT INTO payments (reference,student_id,amount_kobo,tier_unlocked,status) VALUES ('ref-1','ada',500000,'tier_1','success')`,
		"progress day":           `INSERT INTO student_progress (student_id,day_number) VALUES ('ada',1)`,
		"invalid day":            `INSERT INTO student_progress (student_id,day_number) VALUES ('ada',5)`,
		"missing student":        `INSERT INTO student_progress (student_id,day_number) VALUES ('missing',1)`,
		"invalid JSON":           `INSERT INTO activity_logs (student_id,event_type,details) VALUES ('ada','bad','not json')`,
		"negative XP":            `UPDATE students SET xp=-1 WHERE id='ada'`,
		"case insensitive email": `INSERT INTO students (id,full_name,email,consent_timestamp,unsubscribe_token) VALUES ('duplicate','Ada','ADA@example.test',CURRENT_TIMESTAMP,'other')`,
		"missing consent":        `INSERT INTO students (id,full_name,email,unsubscribe_token) VALUES ('no-consent','Ada','new@example.test','new')`,
		"duplicate token":        `INSERT INTO students (id,full_name,email,consent_timestamp,unsubscribe_token) VALUES ('token-copy','Ada','new@example.test',CURRENT_TIMESTAMP,'unsub-ada')`,
	} {
		if _, err := db.ExecContext(ctx, query); err == nil {
			t.Errorf("constraint %s was not enforced", name)
		}
	}
	// Ensure a failed transaction cannot grant a tier without recording payment.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, "UPDATE students SET funnel_tier='tier_1_active' WHERE id='ada'"); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO payments (reference,student_id,amount_kobo,tier_unlocked,status) VALUES ('ref-1','ada',500000,'tier_1','success')`); err == nil {
		t.Fatal("expected duplicate reference to fail")
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT funnel_tier FROM students WHERE id='ada'").Scan(&tier); err != nil || tier != "free_week" {
		t.Fatalf("rollback did not preserve tier: %s %v", tier, err)
	}
	// Reapplying migrations preserves both data and the version ledger.
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil || count != 4 {
		t.Fatalf("migration count=%d err=%v", count, err)
	}
	// Payments retain their student reference until explicitly removed.
	if _, err := db.ExecContext(ctx, "DELETE FROM students WHERE id='ada'"); err == nil {
		t.Fatal("student deletion should respect existing payment reference")
	}
	execSQL(t, db, "DELETE FROM payments WHERE student_id='ada'")
	execSQL(t, db, "DELETE FROM students WHERE id='ada'")
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM student_progress").Scan(&count); err != nil || count != 0 {
		t.Fatalf("progress not cascaded: count=%d err=%v", count, err)
	}
	var student sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT student_id FROM activity_logs").Scan(&student); err != nil || student.Valid {
		t.Fatalf("audit student reference not cleared: %v %v", student, err)
	}
	execSQL(t, db, "DELETE FROM activity_logs")
}

func TestSQLiteReopenAndPool(t *testing.T) {
	file := filepath.Join(t.TempDir(), "students.db")
	first := openSQLite(t, file)
	insertStudent(t, first, "saved", "saved@example.test")
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := Open(context.Background(), Config{Driver: "sqlite3", DSN: file + "?_fk=off&_timeout=0&_journal=off", MaxOpenConns: 4, MaxIdleConns: 3})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if db.Stats().MaxOpenConnections != 4 {
		t.Fatalf("pool config not applied: %+v", db.Stats())
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM students WHERE id='saved'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("reopen lost data: %d %v", count, err)
	}
	// Hold connections simultaneously to force every connection in the pool open.
	connections := make([]*sql.Conn, 0, 4)
	defer func() {
		for _, conn := range connections {
			conn.Close()
		}
	}()
	for i := 0; i < 4; i++ {
		conn, err := db.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, conn)
		if _, err := conn.ExecContext(context.Background(), "INSERT INTO student_progress (student_id,day_number) VALUES ('missing',1)"); err == nil {
			t.Fatalf("connection %d has foreign keys disabled", i)
		}
	}
}

func TestSQLiteMemoryPool(t *testing.T) {
	for _, dsn := range []string{"", ":memory:", "file::memory:?cache=shared", "file:students?mode=memory&cache=shared"} {
		t.Run(dsn, func(t *testing.T) {
			db, err := Open(context.Background(), Config{Driver: "sqlite", DSN: dsn, MaxOpenConns: 10, ConnMaxLifetime: time.Nanosecond})
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if db.Stats().MaxOpenConnections != 1 {
				t.Fatal("memory database must keep one persistent connection")
			}
			insertStudent(t, db, "memory", "memory@example.test")
			if err := db.Migrate(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSQLiteConcurrentStartup(t *testing.T) {
	file := filepath.Join(t.TempDir(), "concurrent.db")
	const workers = 6
	var wg sync.WaitGroup
	results := make(chan error, workers)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			db, err := Open(ctx, Config{Driver: "sqlite", DSN: file})
			if db != nil {
				defer db.Close()
			}
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Error(err)
		}
	}
}

func TestMigrationAppliesPendingVersion(t *testing.T) {
	db := openSQLite(t, ":memory:")
	insertStudent(t, db, "preserved", "preserved@example.test")
	migrations := []migration{
		{1, sqliteSchema},
		{2, sqliteSchemaV2},
		{3, sqliteSchemaV3},
		{4, sqliteSchemaV4},
		{5, "ALTER TABLE students ADD COLUMN timezone TEXT NOT NULL DEFAULT 'UTC';"},
	}
	for i := 0; i < 2; i++ {
		if err := db.migrate(context.Background(), migrations); err != nil {
			t.Fatal(err)
		}
	}
	var zone string
	if err := db.QueryRow("SELECT timezone FROM students WHERE id='preserved'").Scan(&zone); err != nil || zone != "UTC" {
		t.Fatalf("pending migration failed to preserve and extend data: %q %v", zone, err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil || count != 5 {
		t.Fatalf("migration history duplicated or missing: %d %v", count, err)
	}
}

func TestMigrationIntegrityAndRollback(t *testing.T) {
	t.Run("checksum", func(t *testing.T) {
		db := openSQLite(t, ":memory:")
		execSQL(t, db, "UPDATE schema_migrations SET checksum='tampered' WHERE version=1")
		if err := db.Migrate(context.Background()); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
			t.Fatalf("checksum tamper accepted: %v", err)
		}
	})
	t.Run("newer version", func(t *testing.T) {
		db := openSQLite(t, ":memory:")
		execSQL(t, db, "INSERT INTO schema_migrations (version,checksum) VALUES (99,'future')")
		if err := db.Migrate(context.Background()); err == nil || !strings.Contains(err.Error(), "newer than supported") {
			t.Fatalf("unknown version accepted: %v", err)
		}
	})
	t.Run("rollback schema and ledger", func(t *testing.T) {
		db := openSQLite(t, ":memory:")
		err := db.migrate(context.Background(), []migration{
			{1, sqliteSchema},
			{2, sqliteSchemaV2},
			{3, sqliteSchemaV3},
			{4, sqliteSchemaV4},
			{5, "CREATE TABLE rollback_probe (id INTEGER); INSERT INTO table_that_does_not_exist VALUES (1);"},
		})
		if err == nil {
			t.Fatal("invalid migration succeeded")
		}
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil || count != 4 {
			t.Fatalf("failed migration recorded: %d %v", count, err)
		}
		if _, err := db.Exec("SELECT * FROM rollback_probe"); err == nil {
			t.Fatal("partial migration DDL was not rolled back")
		}
		if err := db.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
}

func TestOpenFailures(t *testing.T) {
	for _, cfg := range []Config{
		{Driver: "unknown"},
		{Driver: "postgres"},
		{Driver: "sqlite", MaxOpenConns: -1},
		{Driver: "sqlite", DSN: "file:bad?mode=%zz"},
		{Driver: "sqlite", MaxIdleConns: -1},
		{Driver: "sqlite", ConnMaxLifetime: -time.Second},
		{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "missing", "db.sqlite")},
	} {
		if db, err := Open(context.Background(), cfg); err == nil || db != nil {
			t.Errorf("invalid config unexpectedly opened: %+v", cfg)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if db, err := Open(ctx, Config{Driver: "sqlite"}); err == nil || db != nil {
		t.Fatal("canceled context unexpectedly opened database")
	}
}

func TestPostgresSchemaHelpers(t *testing.T) {
	if !validIdentifier("meuseai") || !validIdentifier("meuse_ai_v2") || !validIdentifier("_private") {
		t.Fatal("expected valid identifiers")
	}
	if validIdentifier("") || validIdentifier("meuse-ai") || validIdentifier("123abc") || validIdentifier("drop schema;") {
		t.Fatal("expected invalid identifiers")
	}
	urlDSN := withPostgresSearchPath("postgres://user:pass@localhost:5432/postgres?sslmode=disable", "meuseai")
	if !strings.Contains(urlDSN, "search_path=meuseai") {
		t.Fatalf("expected search_path in url DSN, got %s", urlDSN)
	}
	kvDSN := withPostgresSearchPath("user=postgres password=secret dbname=postgres", "meuseai")
	if !strings.Contains(kvDSN, "search_path=meuseai") {
		t.Fatalf("expected search_path in key-value DSN, got %s", kvDSN)
	}
}
