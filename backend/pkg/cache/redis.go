// Package cache opens the Redis client used for sessions, login throttling and
// any other short-lived server-side state.
//
// Keys are namespaced by the callers (see the key prefix option of the
// application), not by the client, so that prefixes stay explicit and
// greppable.
package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Options describes a Redis connection.
type Options struct {
	Addr         string
	Username     string
	Password     string
	DB           int
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	PoolSize     int
}

// Open connects to Redis and verifies the connection with a PING.
func Open(ctx context.Context, opts Options) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         opts.Addr,
		Username:     opts.Username,
		Password:     opts.Password,
		DB:           opts.DB,
		DialTimeout:  opts.DialTimeout,
		ReadTimeout:  opts.ReadTimeout,
		WriteTimeout: opts.WriteTimeout,
		PoolSize:     opts.PoolSize,
	})

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return client, nil
}

// Close releases the client.
func Close(client *redis.Client) error {
	if client == nil {
		return nil
	}
	return client.Close()
}
