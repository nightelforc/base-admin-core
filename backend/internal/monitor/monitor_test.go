package monitor

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestChecksWithoutDependencies(t *testing.T) {
	items, err := checks(Deps{})
	if err != nil {
		t.Fatalf("checks(Deps{}) failed: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("checks(Deps{}) returned %d checks, want 0", len(items))
	}
}

func TestChecksIncludesPoolWhenDatabasePresent(t *testing.T) {
	db := &gorm.DB{Config: &gorm.Config{ConnPool: &sql.DB{}}}

	items, err := checks(Deps{DB: db})
	if err != nil {
		t.Fatalf("checks failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("checks returned %d checks, want 1", len(items))
	}
	if name := items[0].Name(); name != "postgres.pool" {
		t.Fatalf("check name = %q, want postgres.pool", name)
	}
}

func TestChecksReportsUnusableDatabaseHandle(t *testing.T) {
	// 连接池不是 *sql.DB 时 gorm 会报错，Run 据此降级为不监控。
	db := &gorm.DB{Config: &gorm.Config{}}

	if _, err := checks(Deps{DB: db}); err == nil {
		t.Fatal("expected an error for a connection pool that is not *sql.DB")
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
