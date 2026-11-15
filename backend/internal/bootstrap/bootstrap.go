// Package bootstrap prepares the data the application cannot start without:
// the first administrator account and its super administrator grant.
package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/base-admin/base-admin-core/backend/internal/config"
	"github.com/base-admin/base-admin-core/backend/internal/model"
	"github.com/base-admin/base-admin-core/backend/internal/repository"
	"github.com/base-admin/base-admin-core/backend/pkg/apperr"
	"github.com/base-admin/base-admin-core/backend/pkg/password"
)

// Deps are the collaborators the bootstrap needs.
type Deps struct {
	Users  *repository.UserRepository
	Roles  *repository.RoleRepository
	Hasher *password.Hasher
	Config config.Bootstrap
	Log    *slog.Logger
}

// Run makes sure the account configured through BOOTSTRAP_* exists and holds
// the super administrator role. It is idempotent and never overwrites an
// existing password.
func Run(ctx context.Context, deps Deps) error {
	username := strings.TrimSpace(deps.Config.AdminUsername)
	if username == "" {
		deps.Log.Info("bootstrap skipped: no admin username configured")
		return nil
	}

	role, err := deps.Roles.FindByCode(ctx, model.RoleCodeSuperAdmin)
	if err != nil {
		if apperr.Is(err, apperr.ErrRoleNotFound) {
			deps.Log.Warn("bootstrap skipped: super_admin role is missing, run the seed migration first")
			return nil
		}
		return fmt.Errorf("find super admin role: %w", err)
	}

	user, err := deps.Users.FindByUsername(ctx, username)
	if err != nil && !apperr.Is(err, apperr.ErrUserNotFound) {
		return fmt.Errorf("find bootstrap admin: %w", err)
	}

	if err == nil {
		if err := deps.Users.EnsureRoleAssignment(ctx, user.ID, role.ID); err != nil {
			return fmt.Errorf("grant super admin role to %s: %w", username, err)
		}
		deps.Log.Info("bootstrap admin already exists", "username", username, "user_id", user.ID)
		return nil
	}

	secret := deps.Config.AdminPassword
	generated := secret == ""
	if generated {
		secret, err = password.Random(16)
		if err != nil {
			return fmt.Errorf("generate bootstrap password: %w", err)
		}
	}
	if err := deps.Hasher.Validate(secret); err != nil {
		return fmt.Errorf("bootstrap admin password rejected: %w", err)
	}

	hash, err := deps.Hasher.Hash(secret)
	if err != nil {
		return fmt.Errorf("hash bootstrap password: %w", err)
	}

	nickname := strings.TrimSpace(deps.Config.AdminNickname)
	if nickname == "" {
		nickname = "超级管理员"
	}

	user = &model.User{
		Username:     username,
		PasswordHash: hash,
		Nickname:     nickname,
		Email:        strings.TrimSpace(deps.Config.AdminEmail),
		Status:       model.StatusEnabled,
	}
	if err := deps.Users.Create(ctx, user); err != nil {
		return fmt.Errorf("create bootstrap admin: %w", err)
	}
	if err := deps.Users.EnsureRoleAssignment(ctx, user.ID, role.ID); err != nil {
		return fmt.Errorf("grant super admin role to %s: %w", username, err)
	}

	// A configured password is already known to the operator; only a generated
	// one has to be surfaced, and only once.
	if generated {
		deps.Log.Warn("bootstrap admin created with a generated password, change it after the first login",
			"username", username, "password", secret)
	} else {
		deps.Log.Info("bootstrap admin created", "username", username, "user_id", user.ID)
	}
	return nil
}
