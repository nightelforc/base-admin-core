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
