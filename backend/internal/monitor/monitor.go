// Package monitor 是本服务的监控模块入口。
//
// 调用方只递运行时依赖，由本包决定开哪些检查：
//
//	if cfg.Monitor.Enabled {
//		monitor.Run(ctx, log, monitor.Deps{DB: db})
//	}
//
// 「监控什么、多久一次、什么算异常」是 monitor 自己的策略，不散落到 cmd 的装配里，
// 也不做成全局配置。新增一个被监控的组件时，在 checks 里加一行即可，cmd 不用改。
//
// 契约与调度在 monitor/core，具体监控项在 monitor/pg 等子包；本包只做两件事：
// 选检查（策略）、按依赖装配（Run）。
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
// 收 *gorm.DB 而不是 *sql.DB：调用方只递它手上有的东西，字段为 nil 时依赖它的检查不注册。
type Deps struct {
	DB *gorm.DB
}

// Run 启动监控后立即返回。
//
// 协程的创建全部在本包内完成：一个调度协程（core.Registry.Run）加每个监控项一个
// 子协程。调用方不写 go，也不等它退出——生命周期跟随 ctx。
//
// 监控永远不是启动的前置条件：依赖不可用时降级为「不监控」并留一条 warning，
// 不会让进程失败；没有可用检查时同样直接返回，不起任何协程。
func Run(ctx context.Context, log *slog.Logger, deps Deps) {
	if log == nil {
		log = slog.Default()
	}

	items, err := checks(deps)
	if err != nil {
		log.Warn("monitor disabled: cannot access database handle", "error", err)
		return
	}
	if len(items) == 0 {
		log.Info("monitor disabled: no checks registered")
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

// checks 是监控项的开关表：这一版只开连接池，新增组件在这里加一行。
// 句柄转换、默认策略都是各监控项自己的事，这里只管「给了什么就开什么」。
func checks(deps Deps) ([]core.Check, error) {
	if deps.DB == nil {
		return nil, nil
	}
	return pg.Checks(deps.DB)
}
