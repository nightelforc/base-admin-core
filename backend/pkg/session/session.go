// Package session 把登录会话保存在 Redis 中。
//
// 会话是已签发令牌对的服务端那一半：令牌携带会话 id，因此删除该记录即可吊销
// 由它派生的全部令牌。登出和强制下线因此立即生效，而不必等待访问令牌过期。
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

	"github.com/nightelforc/base-admin-core/backend/pkg/apperr"
)

// Session 是存储的记录。
type Session struct {
	ID        string    `json:"id"`
	UserID    int64     `json:"user_id"`
	Username  string    `json:"username"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
	CreatedAt time.Time `json:"created_at"`
}

// Store 把会话存在 "{prefix}session:{id}" 下，并在 "{prefix}user_sessions:{userID}"
// 中跟踪某用户的会话，以便修改密码时一次吊销全部会话。
type Store struct {
	client *redis.Client
	prefix string
	ttl    time.Duration
}

// NewStore 返回会话存储；其记录比刷新令牌稍长寿，使与过期竞态的刷新仍能成功。
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

// Create 存储一条新会话。
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

// Get 返回会话；不存在时返回 apperr.ErrUnauthorized。
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

// Touch 延长会话有效期，在令牌每次刷新时调用。
func (s *Store) Touch(ctx context.Context, id string) error {
	if err := s.client.Expire(ctx, s.key(id), s.ttl).Err(); err != nil {
		return fmt.Errorf("session: touch: %w", err)
	}
	return nil
}

// Delete 吊销一条会话。会话已经不在不算错误，因为登出必须保持幂等；基础设施
// 故障会被返回，避免令牌仍然有效时登出却声称成功。
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

// DeleteAllForUser 吊销某用户的全部会话，用于修改密码之后或账号被禁用时。
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

// NewID 返回一个随机、URL 安全的会话标识。
func NewID() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
