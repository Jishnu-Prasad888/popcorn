package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Jishnu-Prasad888/popcorn/internal/audit"
)

// ErrLastAdmin is returned when a change would leave the server with no
// enabled administrator, so nobody could manage it any more.
var ErrLastAdmin = errors.New("the server needs at least one enabled administrator")

// UserPatch is an administrator's change to an account. Nil fields are left
// as they are.
type UserPatch struct {
	Role     *string
	Password *string
	Enabled  *bool
}

// UpdateUser changes an account's role, password or whether it may sign in.
// A new password or disabling the account signs it out everywhere.
func (s *Service) UpdateUser(ctx context.Context, id string, p UserPatch, meta Meta) (*User, error) {
	u, err := s.UserByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	} else if err != nil {
		return nil, fmt.Errorf("lookup user: %w", err)
	}

	role := u.Role
	if p.Role != nil {
		if role, err = NormalizeRole(*p.Role); err != nil {
			return nil, err
		}
	}
	enabled := u.Enabled
	if p.Enabled != nil {
		enabled = *p.Enabled
	}
	hash := u.PasswordHash
	if p.Password != nil {
		if err := ValidateCredentials(u.Username, *p.Password); err != nil {
			return nil, err
		}
		if hash, err = HashPassword(*p.Password); err != nil {
			return nil, err
		}
	}

	// Demoting or disabling an administrator must leave another one.
	if u.Role == RoleAdmin && u.Enabled && (role != RoleAdmin || !enabled) {
		var others int
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM users WHERE role = 'admin' AND is_enabled = 1 AND id <> ?`, id).Scan(&others); err != nil {
			return nil, fmt.Errorf("count admins: %w", err)
		}
		if others == 0 {
			return nil, ErrLastAdmin
		}
	}

	now := rfc3339(time.Now().UTC())
	en := 0
	if enabled {
		en = 1
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET role = ?, is_enabled = ?, password_hash = ?, updated_at = ? WHERE id = ?`,
		string(role), en, hash, now, id); err != nil {
		return nil, fmt.Errorf("update user: %w", err)
	}
	if p.Password != nil || !enabled {
		if _, err := tx.ExecContext(ctx,
			`UPDATE sessions SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`, now, id); err != nil {
			return nil, fmt.Errorf("revoke sessions: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	action := audit.ActionUserUpdated
	switch {
	case u.Enabled && !enabled:
		action = audit.ActionUserDisabled
	case !u.Enabled && enabled:
		action = audit.ActionUserEnabled
	}
	s.audit.Record(ctx, audit.Event{
		ActorUserID:  actorIDFrom(ctx),
		TargetUserID: id,
		Action:       action,
		Details:      map[string]any{"role": string(role), "password_changed": p.Password != nil, "enabled": enabled},
		IPAddress:    meta.IPAddress,
		UserAgent:    meta.UserAgent,
	})
	s.logger.Info("user updated", "user_id", id, "role", role, "enabled", enabled, "password_changed", p.Password != nil)

	u.Role, u.Enabled, u.PasswordHash = role, enabled, hash
	return u, nil
}
