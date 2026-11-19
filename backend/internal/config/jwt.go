package config

import "time"

// JWT 保存令牌签名配置。
type JWT struct {
	Secret     string        `env:"SECRET" validate:"required,min=32" yaml:"secret"`
	Issuer     string        `env:"ISSUER" yaml:"issuer"`
	Audience   string        `env:"AUDIENCE" yaml:"audience"`
	AccessTTL  time.Duration `env:"ACCESS_TTL" yaml:"access_ttl"`
	RefreshTTL time.Duration `env:"REFRESH_TTL" yaml:"refresh_ttl"`
}

// validate 校验 TTL 为正且 refresh 必须长于 access。
func (j JWT) validate() problems {
	var p problems
	if j.AccessTTL <= 0 || j.RefreshTTL <= 0 {
		p.add("JWT_ACCESS_TTL and JWT_REFRESH_TTL must be positive")
	}
	if j.RefreshTTL <= j.AccessTTL {
		p.add("JWT_REFRESH_TTL must be greater than JWT_ACCESS_TTL")
	}
	return p
}
