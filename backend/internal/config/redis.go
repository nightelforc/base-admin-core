package config

import (
	"time"

	"github.com/nightelforc/base-admin-core/backend/pkg/redis"
)

// Redis 保存 Redis 连接设置。
type Redis struct {
	Addr         string        `env:"ADDR" envDefault:"127.0.0.1:6379" validate:"required" yaml:"addr"`
	Username     string        `env:"USERNAME" yaml:"username"`
	Password     string        `env:"PASSWORD" yaml:"password"`
	DB           int           `env:"DB" envDefault:"0" validate:"min=0" yaml:"db"`
	KeyPrefix    string        `env:"KEY_PREFIX" envDefault:"base-admin:" yaml:"key_prefix"`
	DialTimeout  time.Duration `env:"DIAL_TIMEOUT" envDefault:"5s" yaml:"dial_timeout"`
	ReadTimeout  time.Duration `env:"READ_TIMEOUT" envDefault:"3s" yaml:"read_timeout"`
	WriteTimeout time.Duration `env:"WRITE_TIMEOUT" envDefault:"3s" yaml:"write_timeout"`
	PoolSize     int           `env:"POOL_SIZE" envDefault:"20" validate:"min=1" yaml:"pool_size"`
}

// Options 把 Redis 配置转换成客户端的连接参数。
//
// 映射放在这里而不是 main：配置项与环境变量的对应关系只有本包知道，新增字段时
// 只需同步这一个方法，不会出现"加了 env 却忘了传给客户端"的静默失效。Redis 的
// Options 全是纯数据，所以这里是完整映射，不需要像 Database 那样留运行时字段。
func (r Redis) Options() redis.Options {
	return redis.Options{
		Addr:         r.Addr,
		Username:     r.Username,
		Password:     r.Password,
		DB:           r.DB,
		DialTimeout:  r.DialTimeout,
		ReadTimeout:  r.ReadTimeout,
		WriteTimeout: r.WriteTimeout,
		PoolSize:     r.PoolSize,
	}
}
