// Package migrate 按顺序恰好应用一次内嵌的 SQL 迁移。
//
// 设计说明：
//   - SQL 文件是唯一事实来源（不用 AutoMigrate），因此物理 schema 可评审，
//     且在各环境中保持一致。
//   - 每个文件都以 {{prefix}} 为模板；执行器替换配置的 DB_TABLE_PREFIX，
//     使多套部署可以共用同一个 PostgreSQL schema。
//   - 每个文件在一个事务内执行，并以 SHA-256 校验和记录。已应用文件的校验和
//     发生变化是硬错误：绝不修改已应用的迁移，应新增一条。
//   - 以迁移表为键的 PostgreSQL 咨询锁防止两个副本在启动时并发迁移。
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

// PrefixPlaceholder 是由配置的表前缀替换的占位记号。
const PrefixPlaceholder = "{{prefix}}"

// prefixPattern 约束标识符插值：前缀会原样进入 DDL，因此除小写标识符外一律拒绝。
var prefixPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$|^$`)

// File 是一条准备应用的迁移。
type File struct {
	Name     string
	SQL      string
	Checksum string
}

// Options 描述一次迁移运行。
type Options struct {
	// FS 持有 .sql 迁移文件；由调用方嵌入。
	FS fs.FS
	// DSN 是 PostgreSQL 连接串。
	DSN string
	// Prefix 替换 {{prefix}}，并用于命名记账表。
	Prefix string
	// Log 接收进度与失败记录；为 nil 时回落到 slog.Default。
	Log *slog.Logger
}

// Run 应用全部待执行迁移，并返回执行了多少条。
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
	// 简单协议允许把整个迁移文件（多条语句）作为事务内的一次 Exec 发送。
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

	// 串行化并发的迁移执行器。锁由延迟的解锁释放（或在会话结束时自动释放）。
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

// Load 读取、排序并渲染每一条内嵌迁移。
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

// Render 替换表前缀占位符。
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

// quoteIdentifier 渲染一个可信（已经校验过）的标识符。
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func short(checksum string) string {
	if len(checksum) <= 12 {
		return checksum
	}
	return checksum[:12]
}
