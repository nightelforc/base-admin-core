package config

import "time"

// HTTP 保存 HTTP 服务的调优参数。
type HTTP struct {
	Addr              string        `env:"ADDR" envDefault:":8080" yaml:"addr"`
	ReadHeaderTimeout time.Duration `env:"READ_HEADER_TIMEOUT" envDefault:"10s" yaml:"read_header_timeout"`
	ReadTimeout       time.Duration `env:"READ_TIMEOUT" envDefault:"30s" yaml:"read_timeout"`
	WriteTimeout      time.Duration `env:"WRITE_TIMEOUT" envDefault:"60s" yaml:"write_timeout"`
	// RequestTimeout 限制单个请求从进入服务器到处理完成的时长；超时返回 504 并取消
	// 请求上下文；0 表示不限制。
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
	// HTTP_IDLE_TIMEOUT 允许为 0，此时 net/http 回退到 ReadTimeout。
	for _, item := range []struct {
		name  string
		value time.Duration
	}{
		{"HTTP_READ_HEADER_TIMEOUT", h.ReadHeaderTimeout},
		{"HTTP_READ_TIMEOUT", h.ReadTimeout},
		{"HTTP_WRITE_TIMEOUT", h.WriteTimeout},
		{"HTTP_SHUTDOWN_TIMEOUT", h.ShutdownTimeout},
	} {
		if item.value <= 0 {
			p.addf("%s must be positive", item.name)
		}
	}
	if h.IdleTimeout < 0 {
		p.add("HTTP_IDLE_TIMEOUT must not be negative")
	}
	// RequestTimeout 取 0 表示不限制；启用时必须早于 WriteTimeout，否则连接会先被
	// 写超时掐断，504 响应发不出去。
	if h.RequestTimeout < 0 {
		p.add("HTTP_REQUEST_TIMEOUT must not be negative")
	}
	if h.RequestTimeout > 0 && h.WriteTimeout > 0 && h.RequestTimeout >= h.WriteTimeout {
		p.add("HTTP_REQUEST_TIMEOUT must be less than HTTP_WRITE_TIMEOUT")
	}
	return p
}
