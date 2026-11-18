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

func TestNewClientRejectsUnknownMode(t *testing.T) {
	if _, err := newClient(Options{Mode: "cluster"}); err == nil {
		t.Fatal("newClient(未支持的模式) 返回 nil，期望错误")
	}
}

func TestNewClientRejectsEmptyMode(t *testing.T) {
	// 空模式不回落到单机：配置缺失应该显式报错。
	if _, err := newClient(Options{Addrs: []string{"127.0.0.1:6379"}}); err == nil {
		t.Fatal("newClient(空模式) 返回 nil，期望错误")
	}
}

func TestNewClientStandaloneRequiresAddr(t *testing.T) {
	if _, err := newClient(Options{Mode: ModeStandalone}); err == nil {
		t.Fatal("newClient(空地址) 返回 nil，期望错误")
	}
}

func TestNewClientStandaloneRejectsMultipleAddrs(t *testing.T) {
	_, err := newClient(Options{
		Mode:  ModeStandalone,
		Addrs: []string{"127.0.0.1:6379", "127.0.0.1:6380"},
	})
	if err == nil {
		t.Fatal("newClient(单机多地址) 返回 nil，期望错误")
	}
}

func TestNewClientSentinelRequiresMasterName(t *testing.T) {
	_, err := newClient(Options{
		Mode:  ModeSentinel,
		Addrs: []string{"127.0.0.1:26379"},
	})
	if err == nil {
		t.Fatal("newClient(缺主节点名) 返回 nil，期望错误")
	}
}

func TestNewClientSentinelRequiresAddrs(t *testing.T) {
	_, err := newClient(Options{
		Mode:       ModeSentinel,
		MasterName: "mymaster",
	})
	if err == nil {
		t.Fatal("newClient(缺哨兵地址) 返回 nil，期望错误")
	}
}

func TestNewClientSentinelBuildsClient(t *testing.T) {
	// 构造哨兵客户端不会拨号，因此这里不需要真实的哨兵实例。
	client, err := newClient(Options{
		Mode:       ModeSentinel,
		MasterName: "mymaster",
		Addrs:      []string{"127.0.0.1:26379"},
	})
	if err != nil {
		t.Fatalf("newClient(哨兵) 返回错误: %v", err)
	}
	if client == nil {
		t.Fatal("newClient(哨兵) 返回 nil 客户端")
	}
	if err := client.Close(); err != nil {
		t.Fatalf("关闭客户端失败: %v", err)
	}
}
