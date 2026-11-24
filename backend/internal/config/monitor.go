package config

// Monitor 控制内置监控模块的开关。
//
// 这里只放运维需要按环境切换的项：「监控哪些组件、用什么阈值、多久采样一次」属于
// monitor 包自己的策略，不通过配置暴露。默认值只来自 config.yaml；删掉键就是 false
// （关闭），因此关闭时启动日志会留一条记录，避免监控悄悄消失。
type Monitor struct {
	Enabled bool `env:"ENABLED" yaml:"enabled"`
}
