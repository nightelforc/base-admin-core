package config

import "time"

// CORS 保存跨域策略。前端通常经开发服务器代理访问 API，只有前后端分离部署时才需要。
type CORS struct {
	// AllowedOrigins 支持 "*"；但它不能与 AllowCredentials 同时开启，生产环境启动校验会拒绝。
	AllowedOrigins   []string      `env:"ALLOWED_ORIGINS" envSeparator:"," envDefault:"*" yaml:"allowed_origins"`
	AllowedMethods   []string      `env:"ALLOWED_METHODS" envSeparator:"," envDefault:"GET,POST,PUT,PATCH,DELETE,OPTIONS" yaml:"allowed_methods"`
	AllowedHeaders   []string      `env:"ALLOWED_HEADERS" envSeparator:"," envDefault:"Origin,Content-Type,Accept,Authorization,X-Request-ID,X-Requested-With" yaml:"allowed_headers"`
	ExposeHeaders    []string      `env:"EXPOSE_HEADERS" envSeparator:"," envDefault:"X-Request-ID" yaml:"expose_headers"`
	AllowCredentials bool          `env:"ALLOW_CREDENTIALS" envDefault:"false" yaml:"allow_credentials"`
	MaxAge           time.Duration `env:"MAX_AGE" envDefault:"12h" yaml:"max_age"`
}
