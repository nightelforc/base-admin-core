package config

import "time"

// App 保存应用标识。
type App struct {
	Name     string `env:"NAME" yaml:"name"`
	Env      string `env:"ENV" validate:"required,oneof=development test production" yaml:"env"`
	Timezone string `env:"TIMEZONE" yaml:"timezone"`
	Debug    bool   `env:"DEBUG" yaml:"debug"`
}

// IsProduction 报告进程是否使用生产环境配置。
func (a App) IsProduction() bool { return a.Env == EnvProduction }

// validate 校验应用时区可被 time.LoadLocation 加载。
func (a App) validate() problems {
	var p problems
	if _, err := time.LoadLocation(a.Timezone); err != nil {
		p.addf("APP_TIMEZONE %q is not a known IANA time zone", a.Timezone)
	}
	return p
}
