package service

import (
	"context"
	"time"

	"github.com/nightelforc/base-admin-core/backend/internal/model"
	"github.com/nightelforc/base-admin-core/backend/internal/repository"
)

// TrendPoint is one day of the dashboard activity sparkline.
type TrendPoint struct {
	Date    string `json:"date"`
	Total   int64  `json:"total"`
	Success int64  `json:"success"`
	Failed  int64  `json:"failed"`
}

// Overview is the dashboard payload.
type Overview struct {
	UserTotal        int64        `json:"userTotal"`
	UserEnabled      int64        `json:"userEnabled"`
	RoleTotal        int64        `json:"roleTotal"`
	MenuTotal        int64        `json:"menuTotal"`
	LoginToday       int64        `json:"loginToday"`
	LoginFailedToday int64        `json:"loginFailedToday"`
	OperationToday   int64        `json:"operationToday"`
	Trend            []TrendPoint `json:"trend"`
	GeneratedAt      time.Time    `json:"generatedAt"`
}

// DashboardService aggregates the numbers shown on the workspace page.
type DashboardService struct {
	users *repository.UserRepository
	roles *repository.RoleRepository
	menus *repository.MenuRepository
	logs  *repository.LogRepository
	loc   *time.Location
	now   func() time.Time
}

// NewDashboardService wires the dashboard queries. loc decides where "today"
// starts, so an administrator's day matches their configured time zone.
func NewDashboardService(
	users *repository.UserRepository,
	roles *repository.RoleRepository,
	menus *repository.MenuRepository,
	logs *repository.LogRepository,
	loc *time.Location,
) *DashboardService {
	if loc == nil {
		loc = time.UTC
	}
	return &DashboardService{users: users, roles: roles, menus: menus, logs: logs, loc: loc, now: time.Now}
}

// Overview collects counters and the last seven days of login activity.
func (s *DashboardService) Overview(ctx context.Context) (*Overview, error) {
	now := s.now().In(s.loc)
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.loc).UTC()
	sinceTrend := startOfDay.AddDate(0, 0, -6)

	overview := &Overview{GeneratedAt: now.UTC()}

	var err error
	if overview.UserTotal, err = s.users.Count(ctx); err != nil {
		return nil, err
	}
	if overview.UserEnabled, err = s.users.CountByStatus(ctx, model.StatusEnabled); err != nil {
		return nil, err
	}
	if overview.RoleTotal, err = s.roles.Count(ctx); err != nil {
		return nil, err
	}
	if overview.MenuTotal, err = s.menus.Count(ctx); err != nil {
		return nil, err
	}
	if overview.LoginToday, err = s.logs.CountLoginsSince(ctx, startOfDay); err != nil {
		return nil, err
	}
	if overview.LoginFailedToday, err = s.logs.CountLoginFailuresSince(ctx, startOfDay); err != nil {
		return nil, err
	}
	if overview.OperationToday, err = s.logs.CountOperationsSince(ctx, startOfDay); err != nil {
		return nil, err
	}

	points, err := s.logs.LoginTrend(ctx, sinceTrend)
	if err != nil {
		return nil, err
	}
	// 补齐没有登录记录的日期，前端图表始终拿到固定 7 个点。
	byDate := make(map[string]repository.TrendPoint, len(points))
	for _, point := range points {
		byDate[point.Date] = point
	}
	overview.Trend = make([]TrendPoint, 0, 7)
	for i := 6; i >= 0; i-- {
		day := startOfDay.AddDate(0, 0, -i).In(s.loc)
		key := day.Format("2006-01-02")
		point := byDate[key]
		overview.Trend = append(overview.Trend, TrendPoint{
			Date:    key,
			Total:   point.Total,
			Success: point.Success,
			Failed:  point.Failed,
		})
	}
	return overview, nil
}
