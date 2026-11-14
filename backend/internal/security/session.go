package security

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/base-admin/base-admin-core/backend/internal/apperr"
)

// Session is the server-side half of an issued token pair. Tokens carry the
// session id; deleting the record revokes every token derived from it, which is
// what makes logout and forced sign-out immediate instead of waiting for the
// access token to expire.
type Session struct {
	ID        string    `json:"id"`
	UserID    int64     `json:"user_id"`
	Username  string    `json:"username"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
	CreatedAt time.Time `json:"created_at"`
}

// SessionStore keeps sessions in Redis under "{prefix}session:{id}" and tracks
// the sessions of a user in "{prefix}user_sessions:{userID}" so a password
// change can revoke all of them at once.
type SessionStore struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

// NewSessionStore returns a store whose records outlive the refresh token by a
// little so a refresh racing with expiry still succeeds.
func NewSessionStore(client *redis.Client, keyPrefix string, ttl time.Duration) *SessionStore {
	return &SessionStore{
		client: client,
		prefix: keyPrefix,
		ttl:    ttl + time.Minute,
	}
}

func (s *SessionStore) key(id string) string { return s.prefix + "session:" + id }

func (s *SessionStore) userKey(userID int64) string {
	return s.prefix + "user_sessions:" + strconv.FormatInt(userID, 10)
}

// Create stores a new session.
func (s *SessionStore) Create(ctx context.Context, session Session) error {
	payload, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("session: encode: %w", err)
	}

	pipe := s.client.TxPipeline()
	pipe.Set(ctx, s.key(session.ID), payload, s.ttl)
	pipe.SAdd(ctx, s.userKey(session.UserID), session.ID)
	pipe.Expire(ctx, s.userKey(session.UserID), s.ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("session: create: %w", err)
	}
	return nil
}

// Get returns the session or apperr.ErrUnauthorized when it is missing.
func (s *SessionStore) Get(ctx context.Context, id string) (*Session, error) {
	if id == "" {
		return nil, apperr.ErrUnauthorized
	}

	raw, err := s.client.Get(ctx, s.key(id)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, apperr.ErrUnauthorized
	}
	if err != nil {
		return nil, fmt.Errorf("session: get: %w", err)
	}

	var session Session
	if err := json.Unmarshal([]byte(raw), &session); err != nil {
		return nil, fmt.Errorf("session: decode: %w", err)
	}
	return &session, nil
}

// Touch extends the session lifetime, called whenever a token is refreshed.
func (s *SessionStore) Touch(ctx context.Context, id string) error {
	if err := s.client.Expire(ctx, s.key(id), s.ttl).Err(); err != nil {
		return fmt.Errorf("session: touch: %w", err)
	}
	return nil
}

// Delete revokes one session.
func (s *SessionStore) Delete(ctx context.Context, id string) error {
	session, err := s.Get(ctx, id)
	if err != nil {
		// Already gone is not an error for a logout.
		return nil
	}

	pipe := s.client.TxPipeline()
	pipe.Del(ctx, s.key(id))
	pipe.SRem(ctx, s.userKey(session.UserID), id)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("session: delete: %w", err)
	}
	return nil
}

// DeleteAllForUser revokes every session of a user, used after a password change
// or when an account is disabled.
func (s *SessionStore) DeleteAllForUser(ctx context.Context, userID int64) error {
	ids, err := s.client.SMembers(ctx, s.userKey(userID)).Result()
	if err != nil {
		return fmt.Errorf("session: list user sessions: %w", err)
	}

	pipe := s.client.TxPipeline()
	for _, id := range ids {
		pipe.Del(ctx, s.key(id))
	}
	pipe.Del(ctx, s.userKey(userID))
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("session: delete user sessions: %w", err)
	}
	return nil
}
