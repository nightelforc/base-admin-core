package config

import (
	"strings"
	"time"
)

// App 保存应用标识。
type App struct {
	Name     string `env:"NAME" yaml:"name"`
	Env      string `env:"ENV" validate:"required,oneof=development test production" yaml:"env"`
	Timezone string `env:"TIMEZONE" yaml:"timezone"`
	Debug    bool   `env:"DEBUG" yaml:"debug"`
}

// IsProduction 报告进程是否使用生产环境配置。
func (a App) IsProduction() bool { return a.Env == EnvProduction }

// validate 校验应用时区可被 time.LoadLocation 加载，并保证应用名能作为
// PostgreSQL 会话标签完整送达。
func (a App) validate() problems {
	var p problems
	if _, err := time.LoadLocation(a.Timezone); err != nil {
		p.addf("APP_TIMEZONE %q is not a known IANA time zone", a.Timezone)
	}
	// app.name 同时是日志里的应用标识和 PostgreSQL 的会话标签（见 Database.AppDSN），
	// 空白名会让两处都失去辨识度，所以不允许为空。
	if strings.TrimSpace(a.Name) == "" {
		p.add("APP_NAME must not be empty: it is the log identity and the PostgreSQL session label")
	}
	// 超长会被服务端静默截断；迁移连接还要再追加 -migrate，所以按最长的那条算。
	if limit := sessionLabelMaxBytes - len(migrateSessionSuffix); len(a.Name) > limit {
		p.addf("APP_NAME must be at most %d bytes so the PostgreSQL session label fits in %d (the migrate connection appends %q)",
			limit, sessionLabelMaxBytes, migrateSessionSuffix)
	}
	return p
}
