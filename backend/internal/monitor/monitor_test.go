package monitor

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	goredis "github.com/redis/go-redis/v9"

	"github.com/nightelforc/base-admin-core/backend/internal/monitor/core"
)

func TestChecksWithoutDependencies(t *testing.T) {
	items, failures := checks(sources, Deps{})
	if len(items) != 0 {
		t.Fatalf("checks(sources, Deps{}) returned %d checks, want 0", len(items))
	}
	if len(failures) != 0 {
		t.Fatalf("checks(sources, Deps{}) reported %d failures, want 0", len(failures))
	}
}

func TestChecksIncludesPoolWhenDatabasePresent(t *testing.T) {
	db := &gorm.DB{Config: &gorm.Config{ConnPool: &sql.DB{}}}

	items, failures := checks(sources, Deps{DB: db})
	if len(failures) != 0 {
		t.Fatalf("unexpected failures: %v", failures)
	}
	if len(items) != 1 {
		t.Fatalf("checks returned %d checks, want 1", len(items))
	}
	if name := items[0].Name(); name != "postgres.pool" {
		t.Fatalf("check name = %q, want postgres.pool", name)
	}
}

func TestChecksIncludesEveryConfiguredSource(t *testing.T) {
	db := &gorm.DB{Config: &gorm.Config{ConnPool: &sql.DB{}}}
	client := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()

	items, failures := checks(sources, Deps{DB: db, Redis: client})
	if len(failures) != 0 {
		t.Fatalf("unexpected failures: %v", failures)
	}

	names := make([]string, 0, len(items))
	for _, c := range items {
		names = append(names, c.Name())
	}
	if len(names) != 2 || names[0] != "postgres.pool" || names[1] != "redis.pool" {
		t.Fatalf("check names = %v, want [postgres.pool redis.pool]", names)
	}
}

func TestChecksReportsUnusableDatabaseHandle(t *testing.T) {
	// 连接池不是 *sql.DB 时 gorm 会报错：这一路不注册，Run 据此降级。
	items, failures := checks(sources, Deps{DB: &gorm.DB{Config: &gorm.Config{}}})

	if len(items) != 0 {
		t.Fatalf("checks returned %d checks, want 0", len(items))
	}
	if len(failures) != 1 {
		t.Fatalf("failures = %v, want exactly one", failures)
	}
	if failures[0].source != "postgres" || failures[0].err == nil {
		t.Fatalf("failure = %+v, want postgres with an error", failures[0])
	}
}

func TestChecksKeepsGoingAfterASourceFailure(t *testing.T) {
	broken := source{
		name: "broken",
		build: func(Deps) ([]core.Check, error) {
			return nil, errors.New("no handle")
		},
	}
	healthy := source{
		name: "healthy",
		build: func(Deps) ([]core.Check, error) {
			return []core.Check{core.NewPoll("fake", 0, func(context.Context) (int, error) {
				return 0, nil
			}, nil)}, nil
		},
	}

	items, failures := checks([]source{broken, healthy}, Deps{})
	if len(items) != 1 {
		t.Fatalf("checks returned %d checks, want the healthy source to survive", len(items))
	}
	if len(failures) != 1 || failures[0].source != "broken" {
		t.Fatalf("failures = %+v, want only the broken source", failures)
	}
}

func TestRunReturnsWhenThereIsNothingToMonitor(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		Run(context.Background(), nil, Deps{})
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run must return immediately when no check is registered")
	}
}
