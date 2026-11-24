// Package monitor 是本服务的监控模块入口。
//
// 调用方只递运行时依赖，由本包决定开哪些检查：
//
//	if cfg.Monitor.Enabled {
//		monitor.Run(ctx, log, monitor.Deps{DB: db})
//	}
//
// 「监控什么、多久一次、什么算异常」是 monitor 自己的策略，不散落到 cmd 的装配里，
// 也不做成全局配置。新增一个被监控的组件时，在 sources 表里加一行即可，cmd 不用改。
//
// 契约与调度在 monitor/core，具体监控项在 monitor/pg 等子包；本包只做两件事：
// 选来源（sources）、按依赖装配（Run）。
package monitor

import (
	"context"
	"log/slog"

	"gorm.io/gorm"

	"github.com/nightelforc/base-admin-core/backend/internal/monitor/core"
	"github.com/nightelforc/base-admin-core/backend/internal/monitor/pg"
)

// Check 与 Result 是监控项契约的别名，调用方只 import 本包也能写监控项。
type (
	Check  = core.Check
	Result = core.Result
)

// Deps 是调用方能提供的运行时依赖。
//
// 收 *gorm.DB 而不是 *sql.DB：调用方只递它手上有的东西。字段为 nil 时对应来源
// 不注册，这不算失败——没配就是没配。
type Deps struct {
	DB *gorm.DB
}

// source 是一个监控来源：一路依赖，以及把它变成监控项的方式。
//
// 加一个组件就是在 sources 里加一行。这一路装配失败只跳过这一路，其他来源照常启动。
type source struct {
	name  string
	build func(Deps) ([]core.Check, error)
}

// sourceFailure 记录某一路依赖不可用，由 Run 输出成一条 warning。
type sourceFailure struct {
	source string
	err    error
}

var sources = []source{
	{name: "postgres", build: func(d Deps) ([]core.Check, error) { return pg.Checks(d.DB) }},
}

// Run 启动监控后立即返回。
//
// 协程的创建全部在本包内完成：一个调度协程（core.Registry.Run）加每个监控项一个
// 子协程。调用方不写 go，也不等它退出——生命周期跟随 ctx。
//
// 监控永远不是启动的前置条件：某一路依赖不可用时跳过这一路并留一条 warning，
// 不会让进程失败；没有任何可用检查时直接返回，不起任何协程。
func Run(ctx context.Context, log *slog.Logger, deps Deps) {
	if log == nil {
		log = slog.Default()
	}

	items, failures := checks(sources, deps)
	for _, f := range failures {
		log.Warn("monitor source unavailable", "source", f.source, "error", f.err)
	}

	if len(items) == 0 {
		if len(failures) == 0 {
			log.Info("monitor disabled: no checks registered")
		} else {
			log.Info("monitor disabled: no checks available")
		}
		return
	}

	names := make([]string, 0, len(items))
	for _, c := range items {
		names = append(names, c.Name())
	}
	log.Info("monitor started", "checks", names)

	registry := core.New(log)
	registry.Add(items...)
	go registry.Run(ctx)
}

// checks 按来源表装配监控项：失败只记录、不中断，其余来源照常返回。
//
// 表作为参数传入，是为了让「一个源坏了不影响别的源」能脱离真实依赖测试；
// 生产路径只有 Run 传 sources 这一个调用点。
func checks(srcs []source, deps Deps) (items []core.Check, failures []sourceFailure) {
	for _, s := range srcs {
		cs, err := s.build(deps)
		if err != nil {
			failures = append(failures, sourceFailure{source: s.name, err: err})
			continue
		}
		items = append(items, cs...)
	}
	return items, failures
}
