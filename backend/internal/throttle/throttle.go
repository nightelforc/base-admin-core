// Package throttle limits password guessing for the login endpoint.
//
// Failures are counted in Redis so every replica shares the state: after
// maxAttempts failures inside the window for one username/IP pair, further
// attempts are refused for the block duration.
package throttle

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limiter is the login throttle.
type Limiter struct {
	client      *redis.Client
	prefix      string
	maxAttempts int64
	window      time.Duration
	block       time.Duration
}

// NewLimiter builds a limiter; maxAttempts <= 0 disables limiting.
func NewLimiter(client *redis.Client, keyPrefix string, maxAttempts int, window, block time.Duration) *Limiter {
	return &Limiter{
		client:      client,
		prefix:      keyPrefix,
		maxAttempts: int64(maxAttempts),
		window:      window,
		block:       block,
	}
}

func (l *Limiter) attemptsKey(username, ip string) string {
	return l.prefix + "login:attempts:" + username + ":" + ip
}

func (l *Limiter) blockKey(username, ip string) string {
	return l.prefix + "login:block:" + username + ":" + ip
}

// Blocked reports whether the pair is currently locked out and, if so, for how
// long.
func (l *Limiter) Blocked(ctx context.Context, username, ip string) (bool, time.Duration, error) {
	if l.maxAttempts <= 0 {
		return false, 0, nil
	}

	ttl, err := l.client.TTL(ctx, l.blockKey(username, ip)).Result()
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
func (l *Limiter) Fail(ctx context.Context, username, ip string) error {
	if l.maxAttempts <= 0 {
		return nil
	}

	pipe := l.client.TxPipeline()
	countCmd := pipe.Incr(ctx, l.attemptsKey(username, ip))
	pipe.Expire(ctx, l.attemptsKey(username, ip), l.window)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("throttle: count failure: %w", err)
	}

	if countCmd.Val() >= l.maxAttempts {
		if err := l.client.Set(ctx, l.blockKey(username, ip), countCmd.Val(), l.block).Err(); err != nil {
			return fmt.Errorf("throttle: set block: %w", err)
		}
	}
	return nil
}

// Reset clears the failure counter after a successful login.
func (l *Limiter) Reset(ctx context.Context, username, ip string) error {
	if l.maxAttempts <= 0 {
		return nil
	}

	pipe := l.client.TxPipeline()
	pipe.Del(ctx, l.attemptsKey(username, ip))
	pipe.Del(ctx, l.blockKey(username, ip))
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("throttle: reset: %w", err)
	}
	return nil
}

// AttemptsLeft reports how many attempts remain before the lock-out.
func (l *Limiter) AttemptsLeft(ctx context.Context, username, ip string) (int, error) {
	if l.maxAttempts <= 0 {
		return 0, nil
	}

	raw, err := l.client.Get(ctx, l.attemptsKey(username, ip)).Result()
	if err == redis.Nil {
		return int(l.maxAttempts), nil
	}
	if err != nil {
		return 0, fmt.Errorf("throttle: read attempts: %w", err)
	}

	used, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("throttle: decode attempts: %w", err)
	}

	left := int(l.maxAttempts) - used
	if left < 0 {
		left = 0
	}
	return left, nil
}
