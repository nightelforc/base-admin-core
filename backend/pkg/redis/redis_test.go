package redis

import (
	"context"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// 这些用例只验证不依赖真实服务器的失败路径，因此不会发起任何网络连接。

func TestPingNilClientReturnsError(t *testing.T) {
	if err := Ping(context.Background(), nil); err == nil {
		t.Fatal("Ping(nil) 返回 nil，期望错误")
	}
}

func TestPingClosedClientReturnsError(t *testing.T) {
	// Addr 指向一个不会被访问的端口：Close 之后的 Ping 在拨号之前就会失败。
	client := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:1"})
	if err := client.Close(); err != nil {
		t.Fatalf("关闭客户端失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := Ping(ctx, client); err == nil {
		t.Fatal("对已关闭的客户端 Ping 返回 nil，期望错误")
	}
}

func TestHealthCheckerDelegatesToPing(t *testing.T) {
	if err := NewHealthChecker(nil).Ping(context.Background()); err == nil {
		t.Fatal("HealthChecker(nil).Ping 返回 nil，期望错误")
	}
}
