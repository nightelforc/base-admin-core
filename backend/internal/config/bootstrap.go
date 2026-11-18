package config

// Bootstrap 描述首次启动时创建的管理员账号。
type Bootstrap struct {
	// AdminUsername 为空时跳过引导，不创建任何账号。
	AdminUsername string `env:"ADMIN_USERNAME" envDefault:"admin" yaml:"admin_username"`
	// AdminPassword 为空时随机生成，并在启动日志中打印一次。
	AdminPassword string `env:"ADMIN_PASSWORD" yaml:"admin_password"`
	AdminNickname string `env:"ADMIN_NICKNAME" envDefault:"超级管理员" yaml:"admin_nickname"`
	AdminEmail    string `env:"ADMIN_EMAIL" envDefault:"" yaml:"admin_email"`
}
