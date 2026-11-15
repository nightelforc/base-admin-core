package security

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// LoginThrottle limits password guessing: after maxAttempts failures inside the
// window for one username/IP pair, further attempts are refused for the block
// duration. Failures are counted in Redis so every replica shares the state.
type LoginThrottle struct {
	client      *redis.Client
	prefix      string
	maxAttempts int64
	window      time.Duration
	block       time.Duration
}

// NewLoginThrottle builds a throttle; maxAttempts <= 0 disables limiting.
func NewLoginThrottle(client *redis.Client, keyPrefix string, maxAttempts int, window, block time.Duration) *LoginThrottle {
	return &LoginThrottle{
		client:      client,
		prefix:      keyPrefix,
		maxAttempts: int64(maxAttempts),
		window:      window,
		block:       block,
	}
}

func (t *LoginThrottle) attemptsKey(username, ip string) string {
	return t.prefix + "login:attempts:" + username + ":" + ip
}

func (t *LoginThrottle) blockKey(username, ip string) string {
	return t.prefix + "login:block:" + username + ":" + ip
}

// Blocked reports whether the pair is currently locked out and, if so, for how
// long.
func (t *LoginThrottle) Blocked(ctx context.Context, username, ip string) (bool, time.Duration, error) {
	if t.maxAttempts <= 0 {
		return false, 0, nil
	}

	ttl, err := t.client.TTL(ctx, t.blockKey(username, ip)).Result()
	if err != nil {
		return false, 0, fmt.Errorf("throttle: read block: %w", err)
	}
	// A missing key reports -2, an existing key without expiry -1.
	if ttl > 0 {
		return true, ttl, nil
	}
	return false, 0, nil
}

// Fail records a failed attempt and blocks the pair once the limit is reached.
func (t *LoginThrottle) Fail(ctx context.Context, username, ip string) error {
	if t.maxAttempts <= 0 {
		return nil
	}

	pipe := t.client.TxPipeline()
	countCmd := pipe.Incr(ctx, t.attemptsKey(username, ip))
	pipe.Expire(ctx, t.attemptsKey(username, ip), t.window)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("throttle: count failure: %w", err)
	}

	if countCmd.Val() >= t.maxAttempts {
		if err := t.client.Set(ctx, t.blockKey(username, ip), countCmd.Val(), t.block).Err(); err != nil {
			return fmt.Errorf("throttle: set block: %w", err)
		}
	}
	return nil
}

// Reset clears the failure counter after a successful login.
func (t *LoginThrottle) Reset(ctx context.Context, username, ip string) error {
	if t.maxAttempts <= 0 {
		return nil
	}

	pipe := t.client.TxPipeline()
	pipe.Del(ctx, t.attemptsKey(username, ip))
	pipe.Del(ctx, t.blockKey(username, ip))
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("throttle: reset: %w", err)
	}
	return nil
}

// AttemptsLeft reports how many attempts remain before the lock-out.
func (t *LoginThrottle) AttemptsLeft(ctx context.Context, username, ip string) (int, error) {
	if t.maxAttempts <= 0 {
		return 0, nil
	}

	raw, err := t.client.Get(ctx, t.attemptsKey(username, ip)).Result()
	if err == redis.Nil {
		return int(t.maxAttempts), nil
	}
	if err != nil {
		return 0, fmt.Errorf("throttle: read attempts: %w", err)
	}

	used, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("throttle: decode attempts: %w", err)
	}

	left := int(t.maxAttempts) - used
	if left < 0 {
		left = 0
	}
	return left, nil
}
