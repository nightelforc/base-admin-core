package redis

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
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

func TestNewClientCarriesPoolTimeout(t *testing.T) {
	// 两种模式都要传：漏一处就会让那条路径悄悄回落到驱动的推导值
	// （read_timeout + 1s），而配置里写的是别的数。
	const poolTimeout = 750 * time.Millisecond

	for _, tc := range []struct {
		name string
		opts Options
	}{
		{
			name: "standalone",
			opts: Options{Mode: ModeStandalone, Addrs: []string{"127.0.0.1:6379"}, PoolTimeout: poolTimeout},
		},
		{
			name: "sentinel",
			opts: Options{Mode: ModeSentinel, MasterName: "mymaster", Addrs: []string{"127.0.0.1:26379"}, PoolTimeout: poolTimeout},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := newClient(tc.opts)
			if err != nil {
				t.Fatalf("newClient() 返回错误: %v", err)
			}
			defer client.Close()

			if got := client.Options().PoolTimeout; got != poolTimeout {
				t.Fatalf("PoolTimeout = %v, want %v", got, poolTimeout)
			}
		})
	}
}

func TestNewClientTLSDisabledByDefault(t *testing.T) {
	client, err := newClient(Options{Mode: ModeStandalone, Addrs: []string{"127.0.0.1:6379"}})
	if err != nil {
		t.Fatalf("newClient(单机) 返回错误: %v", err)
	}
	defer client.Close()

	if client.Options().TLSConfig != nil {
		t.Fatal("未开启 TLS 却带上了 TLSConfig")
	}
}

func TestNewClientTLSEnabledBuildsConfig(t *testing.T) {
	client, err := newClient(Options{
		Mode:  ModeStandalone,
		Addrs: []string{"127.0.0.1:6379"},
		TLS:   TLSOptions{Enabled: true, ServerName: "redis.internal"},
	})
	if err != nil {
		t.Fatalf("newClient(TLS) 返回错误: %v", err)
	}
	defer client.Close()

	cfg := client.Options().TLSConfig
	if cfg == nil {
		t.Fatal("开启 TLS 后 TLSConfig 为 nil")
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Fatalf("MinVersion = %x，期望 TLS 1.2（%x）", cfg.MinVersion, tls.VersionTLS12)
	}
	if cfg.ServerName != "redis.internal" {
		t.Fatalf("ServerName = %q，期望 redis.internal", cfg.ServerName)
	}
}

func TestNewClientSentinelAppliesSameTLSConfig(t *testing.T) {
	// go-redis 的 FailoverOptions 只有一个 TLSConfig，哨兵与数据连接共用它。
	client, err := newClient(Options{
		Mode:       ModeSentinel,
		MasterName: "mymaster",
		Addrs:      []string{"127.0.0.1:26379"},
		TLS:        TLSOptions{Enabled: true},
	})
	if err != nil {
		t.Fatalf("newClient(哨兵 + TLS) 返回错误: %v", err)
	}
	defer client.Close()

	if client.Options().TLSConfig == nil {
		t.Fatal("哨兵模式开启 TLS 后 TLSConfig 为 nil")
	}
}

func TestNewClientTLSRequiresCertAndKeyTogether(t *testing.T) {
	_, err := newClient(Options{
		Mode:  ModeStandalone,
		Addrs: []string{"127.0.0.1:6379"},
		TLS:   TLSOptions{Enabled: true, CertFile: "client.pem"},
	})
	if err == nil {
		t.Fatal("只配了客户端证书、没配私钥，却没有报错")
	}
}

func TestNewClientTLSRejectsMissingCAFile(t *testing.T) {
	_, err := newClient(Options{
		Mode:  ModeStandalone,
		Addrs: []string{"127.0.0.1:6379"},
		TLS: TLSOptions{
			Enabled: true,
			CAFile:  filepath.Join(t.TempDir(), "missing.pem"),
		},
	})
	if err == nil {
		t.Fatal("CA 文件不存在，却没有报错")
	}
}

func TestNewClientTLSRejectsInvalidCA(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.pem")
	if err := os.WriteFile(path, []byte("not a certificate"), 0o600); err != nil {
		t.Fatalf("写入测试 CA 失败: %v", err)
	}

	_, err := newClient(Options{
		Mode:  ModeStandalone,
		Addrs: []string{"127.0.0.1:6379"},
		TLS:   TLSOptions{Enabled: true, CAFile: path},
	})
	if err == nil || !strings.Contains(err.Error(), "no PEM certificates") {
		t.Fatalf("非法 CA 的错误 = %v，期望包含 no PEM certificates", err)
	}
}

func TestNewClientTLSAcceptsSelfSignedCAAndClientCert(t *testing.T) {
	caFile, certFile, keyFile := writeSelfSignedCert(t)

	client, err := newClient(Options{
		Mode:  ModeStandalone,
		Addrs: []string{"127.0.0.1:6379"},
		TLS: TLSOptions{
			Enabled:  true,
			CAFile:   caFile,
			CertFile: certFile,
			KeyFile:  keyFile,
		},
	})
	if err != nil {
		t.Fatalf("newClient(自签 CA + 客户端证书) 返回错误: %v", err)
	}
	defer client.Close()

	cfg := client.Options().TLSConfig
	if cfg == nil {
		t.Fatal("TLSConfig 为 nil")
	}
	if cfg.RootCAs == nil {
		t.Fatal("配置了 CA 文件，RootCAs 却为 nil")
	}
	if len(cfg.Certificates) != 1 {
		t.Fatalf("Certificates 数量 = %d，期望 1", len(cfg.Certificates))
	}
}

// writeSelfSignedCert 在临时目录生成一套自签证书，返回 CA、客户端证书与私钥的
// 路径。用例只需要"能被 tls 包解析的 PEM"，不涉及握手。
func writeSelfSignedCert(t *testing.T) (caFile, certFile, keyFile string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成私钥失败: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "redis-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("签发证书失败: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("编码私钥失败: %v", err)
	}

	dir := t.TempDir()
	caFile = filepath.Join(dir, "ca.pem")
	certFile = filepath.Join(dir, "client.pem")
	keyFile = filepath.Join(dir, "client.key")

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	for _, item := range []struct {
		path string
		data []byte
	}{
		{caFile, certPEM},
		{certFile, certPEM},
		{keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})},
	} {
		if err := os.WriteFile(item.path, item.data, 0o600); err != nil {
			t.Fatalf("写入 %s 失败: %v", item.path, err)
		}
	}

	return caFile, certFile, keyFile
}
