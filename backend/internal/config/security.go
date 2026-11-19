package config

import "time"

// Security 保存认证加固与审计配置。
type Security struct {
	BcryptCost         int           `env:"BCRYPT_COST" validate:"min=4,max=31" yaml:"bcrypt_cost"`
	PasswordMinLength  int           `env:"PASSWORD_MIN_LENGTH" validate:"min=6,max=64" yaml:"password_min_length"`
	LoginMaxAttempts   int           `env:"LOGIN_MAX_ATTEMPTS" validate:"min=1" yaml:"login_max_attempts"`
	LoginWindow        time.Duration `env:"LOGIN_WINDOW" yaml:"login_window"`
	LoginBlockDuration time.Duration `env:"LOGIN_BLOCK_DURATION" yaml:"login_block_duration"`

	OperationLogEnabled bool `env:"OPERATION_LOG_ENABLED" yaml:"operation_log_enabled"`
	// OperationLogBodyLimit 是操作日志快照最多截取的请求体字节数，避免大请求把审计表撑爆；
	// 它不是 HTTP 请求体上限（那是 HTTP_MAX_BODY_BYTES）。0 表示不记录请求体，
	// 仍记录方法、路径、状态码与耗时。上限与 request_body 列长度一致。
	OperationLogBodyLimit int `env:"OPERATION_LOG_BODY_LIMIT" validate:"min=0,max=4096" yaml:"operation_log_body_limit"`
}

// validate 校验登录窗口与封禁时长必须为正。
func (s Security) validate() problems {
	var p problems
	if s.LoginWindow <= 0 {
		p.add("SECURITY_LOGIN_WINDOW must be positive")
	}
	if s.LoginBlockDuration <= 0 {
		p.add("SECURITY_LOGIN_BLOCK_DURATION must be positive")
	}
	return p
}
