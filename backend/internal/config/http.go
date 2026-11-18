package config

import "time"

// HTTP 保存 HTTP 服务的调优参数。
type HTTP struct {
	Addr              string        `env:"ADDR" envDefault:":8080" yaml:"addr"`
	ReadHeaderTimeout time.Duration `env:"READ_HEADER_TIMEOUT" envDefault:"10s" yaml:"read_header_timeout"`
	ReadTimeout       time.Duration `env:"READ_TIMEOUT" envDefault:"30s" yaml:"read_timeout"`
	// WriteTimeout 是连接级写截止时间，从读完请求头开始计算。它对 SSE / WebSocket
	// 是致命的（到点直接掐断连接），所以默认关闭；不提供长连接时建议改成 60s，
	// 让慢客户端无法长期占用 goroutine。
	WriteTimeout time.Duration `env:"WRITE_TIMEOUT" envDefault:"0s" yaml:"write_timeout"`
	// RequestTimeout 是注入到请求 context 的截止时间（预算），不是强制断点：超时
	// 后 context 被取消，数据库、Redis 与出站调用随之中断，由处理器返回 504；
	// 响应不会被缓冲，也不会被本层改写。0 表示不注入。
	RequestTimeout  time.Duration `env:"REQUEST_TIMEOUT" envDefault:"30s" yaml:"request_timeout"`
	IdleTimeout     time.Duration `env:"IDLE_TIMEOUT" envDefault:"120s" yaml:"idle_timeout"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"20s" yaml:"shutdown_timeout"`
	MaxBodyBytes    int64         `env:"MAX_BODY_BYTES" envDefault:"4194304" yaml:"max_body_bytes"`
	TrustedProxies  []string      `env:"TRUSTED_PROXIES" envSeparator:"," yaml:"trusted_proxies"`
}

// validate 校验超时之间、请求体上限与写超时之间的约束。
func (h HTTP) validate() problems {
	var p problems
	if h.MaxBodyBytes <= 0 {
		p.add("HTTP_MAX_BODY_BYTES must be positive")
	}

	// 超时取 0 或负数时，net/http 会按“不超时”处理，慢连接就可能把进程拖住。
	// HTTP_WRITE_TIMEOUT 是例外：0 表示不设写截止时间，服务长连接时必须如此。
	// HTTP_IDLE_TIMEOUT 允许为 0，此时 net/http 回退到 ReadTimeout。
	for _, item := range []struct {
		name  string
		value time.Duration
	}{
		{"HTTP_READ_HEADER_TIMEOUT", h.ReadHeaderTimeout},
		{"HTTP_READ_TIMEOUT", h.ReadTimeout},
		{"HTTP_SHUTDOWN_TIMEOUT", h.ShutdownTimeout},
	} {
		if item.value <= 0 {
			p.addf("%s must be positive", item.name)
		}
	}
	if h.WriteTimeout < 0 {
		p.add("HTTP_WRITE_TIMEOUT must not be negative")
	}
	if h.IdleTimeout < 0 {
		p.add("HTTP_IDLE_TIMEOUT must not be negative")
	}
	// RequestTimeout 取 0 表示不注入截止时间；启用且设置了写超时时，必须早于
	// WriteTimeout，否则连接会先被写超时掐断，超时响应发不出去。
	if h.RequestTimeout < 0 {
		p.add("HTTP_REQUEST_TIMEOUT must not be negative")
	}
	if h.RequestTimeout > 0 && h.WriteTimeout > 0 && h.RequestTimeout >= h.WriteTimeout {
		p.add("HTTP_REQUEST_TIMEOUT must be less than HTTP_WRITE_TIMEOUT")
	}
	return p
}
