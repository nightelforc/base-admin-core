package config

// Log 保存日志配置。
type Log struct {
	Level     string `env:"LEVEL" validate:"required,oneof=debug info warn error" yaml:"level"`
	Format    string `env:"FORMAT" validate:"required,oneof=text json" yaml:"format"`
	AddSource bool   `env:"ADD_SOURCE" yaml:"add_source"`
}
