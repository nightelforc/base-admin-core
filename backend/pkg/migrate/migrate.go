// Package migrate applies the embedded SQL migrations exactly once, in order.
//
// Design notes:
//   - SQL files are the source of truth (no AutoMigrate), so the physical schema
//     is reviewable and identical across environments.
//   - Every file is templated with {{prefix}}; the runner substitutes the
//     configured DB_TABLE_PREFIX, which makes several deployments shareable in a
//     single PostgreSQL schema.
//   - Each file runs inside one transaction and is recorded with its SHA-256
//     checksum. A changed checksum on an already-applied file is a hard error:
//     never edit an applied migration, add a new one instead.
//   - A PostgreSQL advisory lock keyed on the migrations table prevents two
//     replicas from migrating concurrently at start-up.
package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// PrefixPlaceholder is the token replaced by the configured table prefix.
const PrefixPlaceholder = "{{prefix}}"

// prefixPattern guards identifier interpolation: a prefix ends up verbatim in
// DDL, so anything but lowercase identifiers is rejected.
var prefixPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$|^$`)

// File is a single migration ready to be applied.
type File struct {
	Name     string
	SQL      string
	Checksum string
}

// Options describes one migration run.
type Options struct {
	// FS holds the .sql migration files; embedded by the caller.
	FS fs.FS
	// DSN is the PostgreSQL connection string.
	DSN string
	// Prefix is substituted for {{prefix}} and names the bookkeeping table.
	Prefix string
	// Log receives progress and failure records; nil falls back to slog.Default.
	Log *slog.Logger
}

// Run applies all pending migrations and returns how many were executed.
func Run(ctx context.Context, opts Options) (int, error) {
	if opts.FS == nil {
		return 0, fmt.Errorf("migrate: no migration filesystem provided")
	}
	if !prefixPattern.MatchString(opts.Prefix) {
		return 0, fmt.Errorf("migrate: invalid table prefix %q", opts.Prefix)
	}

	log := opts.Log
	if log == nil {
		log = slog.Default()
	}

	files, err := Load(opts.FS, opts.Prefix)
	if err != nil {
		return 0, err
	}

	connConfig, err := pgx.ParseConfig(opts.DSN)
	if err != nil {
		return 0, fmt.Errorf("migrate: parse database dsn: %w", err)
	}
	// Simple protocol allows a whole migration file (multiple statements) to be
	// sent as one Exec inside the transaction.
	connConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	connectCtx, cancelConnect := context.WithTimeout(ctx, 30*time.Second)
	defer cancelConnect()

	conn, err := pgx.ConnectConfig(connectCtx, connConfig)
	if err != nil {
		return 0, fmt.Errorf("migrate: connect: %w", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = conn.Close(closeCtx)
	}()

	table := opts.Prefix + "schema_migrations"

	// Serialise concurrent migrators. The lock is released by the deferred
	// unlock (or automatically when the session ends).
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(hashtextextended($1, 0))", table); err != nil {
		return 0, fmt.Errorf("migrate: acquire advisory lock: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, "SELECT pg_advisory_unlock(hashtextextended($1, 0))", table)
	}()

	if err := ensureMigrationsTable(ctx, conn, table); err != nil {
		return 0, err
	}

	applied, err := loadApplied(ctx, conn, table)
	if err != nil {
		return 0, err
	}

	pending := 0
	for _, file := range files {
		if checksum, ok := applied[file.Name]; ok {
			if checksum != file.Checksum {
				return pending, fmt.Errorf(
					"migrate: %s was modified after being applied (recorded %s, current %s); add a new migration instead",
					file.Name, short(checksum), short(file.Checksum))
			}
			continue
		}

		started := time.Now()
		if err := applyFile(ctx, conn, table, file); err != nil {
			return pending, err
		}
		pending++
		log.Info("migration applied", "file", file.Name, "duration", time.Since(started).String())
	}

	if pending == 0 {
		log.Info("database schema is up to date")
	}
	return pending, nil
}

// Load reads, sorts and renders every embedded migration.
func Load(fsys fs.FS, prefix string) ([]File, error) {
	if !prefixPattern.MatchString(prefix) {
		return nil, fmt.Errorf("migrate: invalid table prefix %q", prefix)
	}

	names, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("migrate: list migrations: %w", err)
	}
	sort.Strings(names)

	files := make([]File, 0, len(names))
	for _, name := range names {
		raw, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, fmt.Errorf("migrate: read %s: %w", name, err)
		}
		sum := sha256.Sum256(raw)
		files = append(files, File{
			Name:     name,
			SQL:      Render(string(raw), prefix),
			Checksum: hex.EncodeToString(sum[:]),
		})
	}
	return files, nil
}

// Render substitutes the table prefix placeholder.
func Render(script, prefix string) string {
	return strings.ReplaceAll(script, PrefixPlaceholder, prefix)
}

func ensureMigrationsTable(ctx context.Context, conn *pgx.Conn, table string) error {
	stmt := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		filename    text        PRIMARY KEY,
		checksum    text        NOT NULL,
		applied_at  timestamptz NOT NULL DEFAULT now(),
		duration_ms bigint      NOT NULL DEFAULT 0
	)`, quoteIdentifier(table))
	if _, err := conn.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("migrate: create migrations table: %w", err)
	}
	return nil
}

func loadApplied(ctx context.Context, conn *pgx.Conn, table string) (map[string]string, error) {
	rows, err := conn.Query(ctx, fmt.Sprintf("SELECT filename, checksum FROM %s", quoteIdentifier(table)))
	if err != nil {
		return nil, fmt.Errorf("migrate: read applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]string, 8)
	for rows.Next() {
		var name, checksum string
		if err := rows.Scan(&name, &checksum); err != nil {
			return nil, fmt.Errorf("migrate: scan applied migration: %w", err)
		}
		applied[name] = checksum
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("migrate: iterate applied migrations: %w", err)
	}
	return applied, nil
}

func applyFile(ctx context.Context, conn *pgx.Conn, table string, file File) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("migrate: begin %s: %w", file.Name, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	body := strings.TrimSpace(file.SQL)
	if body == "" {
		return fmt.Errorf("migrate: %s is empty", file.Name)
	}

	started := time.Now()
	if _, err := tx.Exec(ctx, body); err != nil {
		return fmt.Errorf("migrate: apply %s: %w", file.Name, err)
	}

	insert := fmt.Sprintf(
		"INSERT INTO %s (filename, checksum, duration_ms) VALUES ($1, $2, $3)",
		quoteIdentifier(table),
	)
	if _, err := tx.Exec(ctx, insert, file.Name, file.Checksum, time.Since(started).Milliseconds()); err != nil {
		return fmt.Errorf("migrate: record %s: %w", file.Name, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("migrate: commit %s: %w", file.Name, err)
	}
	return nil
}

// quoteIdentifier renders a trusted (already validated) identifier.
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func short(checksum string) string {
	if len(checksum) <= 12 {
		return checksum
	}
	return checksum[:12]
}
