// Package repository is the only layer that talks to the database. It exposes
// intent-revealing methods (one per query) and never leaks *gorm.DB to callers,
// so services stay independent from the ORM.
package repository

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"github.com/base-admin/base-admin-core/backend/internal/apperr"
	"github.com/base-admin/base-admin-core/backend/internal/model"
)

// tableNames holds the fully qualified (prefixed) table names. They are derived
// from GORM's naming strategy once at start-up and reused by hand-written joins,
// which cannot rely on the ORM's automatic prefixing.
type tableNames struct {
	users         string
	roles         string
	menus         string
	userRoles     string
	roleMenus     string
	loginLogs     string
	operationLogs string
}

// Base carries the shared dependencies of every repository.
type Base struct {
	db     *gorm.DB
	tables tableNames
}

// NewBase builds the repository base, resolving prefixed table names from the
// GORM schema so the DB_TABLE_PREFIX setting is honoured everywhere.
func NewBase(db *gorm.DB) Base {
	tables := tableNames{}
	assign := func(target *string, value any) {
		stmt := &gorm.Statement{DB: db}
		if err := stmt.Parse(value); err != nil {
			panic(fmt.Sprintf("repository: resolve table name: %v", err))
		}
		*target = stmt.Schema.Table
	}
	assign(&tables.users, &model.User{})
	assign(&tables.roles, &model.Role{})
	assign(&tables.menus, &model.Menu{})
	assign(&tables.userRoles, &model.UserRole{})
	assign(&tables.roleMenus, &model.RoleMenu{})
	assign(&tables.loginLogs, &model.LoginLog{})
	assign(&tables.operationLogs, &model.OperationLog{})

	return Base{db: db, tables: tables}
}

// WithTx returns a copy of the base bound to a transaction.
func (b Base) WithTx(tx *gorm.DB) Base {
	return Base{db: tx, tables: b.tables}
}

// Transaction runs fn inside a database transaction, rolling back on error.
func (b Base) Transaction(ctx context.Context, fn func(tx Base) error) error {
	return b.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(b.WithTx(tx))
	})
}

// DB exposes the request-scoped GORM handle to sub-packages of repository.
func (b Base) DB(ctx context.Context) *gorm.DB { return b.db.WithContext(ctx) }

// Tables exposes the resolved table names to sub-packages of repository.
func (b Base) Tables() tableNames { return b.tables }

// translate converts GORM errors into application errors.
func translate(err error, notFound *apperr.Error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if notFound != nil {
			return notFound
		}
		return apperr.ErrNotFound
	}
	return apperr.ErrInternal.Wrap(err)
}

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	type sqlState interface{ SQLState() string }
	var state sqlState
	if errors.As(err, &state) {
		return state.SQLState() == "23505"
	}
	return false
}

// Page is the pagination window understood by repositories.
type Page struct {
	Offset int
	Limit  int
}

// Normalize clamps the window to sane bounds.
func (p Page) Normalize() Page {
	if p.Limit <= 0 {
		p.Limit = 20
	}
	if p.Limit > 200 {
		p.Limit = 200
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	return p
}
