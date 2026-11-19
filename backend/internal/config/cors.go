package config

import "time"

// CORS 保存跨域策略。前端通常经开发服务器代理访问 API，只有前后端分离部署时才需要。
type CORS struct {
	// AllowedOrigins 支持 "*"；但它不能与 AllowCredentials 同时开启，生产环境启动校验会拒绝。
	AllowedOrigins   []string      `env:"ALLOWED_ORIGINS" envSeparator:"," yaml:"allowed_origins"`
	AllowedMethods   []string      `env:"ALLOWED_METHODS" envSeparator:"," yaml:"allowed_methods"`
	AllowedHeaders   []string      `env:"ALLOWED_HEADERS" envSeparator:"," yaml:"allowed_headers"`
	ExposeHeaders    []string      `env:"EXPOSE_HEADERS" envSeparator:"," yaml:"expose_headers"`
	AllowCredentials bool          `env:"ALLOW_CREDENTIALS" yaml:"allow_credentials"`
	MaxAge           time.Duration `env:"MAX_AGE" yaml:"max_age"`
}

// validate 校验预检缓存时间不能为负；0 表示不缓存。
func (c CORS) validate() problems {
	var p problems
	if c.MaxAge < 0 {
		p.add("CORS_MAX_AGE must not be negative")
	}
	return p
}
