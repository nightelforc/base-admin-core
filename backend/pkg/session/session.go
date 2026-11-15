// Package session keeps login sessions in Redis.
//
// A session is the server-side half of an issued token pair: tokens carry the
// session id, so deleting the record revokes every token derived from it. That
// makes logout and forced sign-out immediate instead of waiting for an access
// token to expire.
package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/base-admin/base-admin-core/backend/pkg/apperr"
)

// Session is the stored record.
type Session struct {
	ID        string    `json:"id"`
	UserID    int64     `json:"user_id"`
	Username  string    `json:"username"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
	CreatedAt time.Time `json:"created_at"`
}

// Store keeps sessions under "{prefix}session:{id}" and tracks the sessions of a
// user in "{prefix}user_sessions:{userID}" so a password change can revoke all
// of them at once.
type Store struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

// NewStore returns a store whose records outlive the refresh token by a little
// so a refresh racing with expiry still succeeds.
func NewStore(client *redis.Client, keyPrefix string, ttl time.Duration) *Store {
	return &Store{
		client: client,
		prefix: keyPrefix,
		ttl:    ttl + time.Minute,
	}
}

func (s *Store) key(id string) string { return s.prefix + "session:" + id }

func (s *Store) userKey(userID int64) string {
	return s.prefix + "user_sessions:" + strconv.FormatInt(userID, 10)
}

// Create stores a new session.
func (s *Store) Create(ctx context.Context, record Session) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("session: encode: %w", err)
	}

	pipe := s.client.TxPipeline()
	pipe.Set(ctx, s.key(record.ID), payload, s.ttl)
	pipe.SAdd(ctx, s.userKey(record.UserID), record.ID)
	pipe.Expire(ctx, s.userKey(record.UserID), s.ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("session: create: %w", err)
	}
	return nil
}

// Get returns the session or apperr.ErrUnauthorized when it is missing.
func (s *Store) Get(ctx context.Context, id string) (*Session, error) {
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

	var record Session
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		return nil, fmt.Errorf("session: decode: %w", err)
	}
	return &record, nil
}

// Touch extends the session lifetime, called whenever a token is refreshed.
func (s *Store) Touch(ctx context.Context, id string) error {
	if err := s.client.Expire(ctx, s.key(id), s.ttl).Err(); err != nil {
		return fmt.Errorf("session: touch: %w", err)
	}
	return nil
}

// Delete revokes one session. A session that is already gone is not an error,
// because logout must stay idempotent; infrastructure failures are returned so a
// logout cannot claim success while the tokens stay valid.
func (s *Store) Delete(ctx context.Context, id string) error {
	record, err := s.Get(ctx, id)
	if err != nil {
		if apperr.Is(err, apperr.ErrUnauthorized) {
			return nil
		}
		return err
	}

	pipe := s.client.TxPipeline()
	pipe.Del(ctx, s.key(id))
	pipe.SRem(ctx, s.userKey(record.UserID), id)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("session: delete: %w", err)
	}
	return nil
}

// DeleteAllForUser revokes every session of a user, used after a password change
// or when an account is disabled.
func (s *Store) DeleteAllForUser(ctx context.Context, userID int64) error {
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

// NewID returns a random, URL-safe session identifier.
func NewID() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
