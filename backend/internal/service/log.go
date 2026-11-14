package service

import (
	"context"
	"time"

	"github.com/base-admin/base-admin-core/backend/internal/repository"
)

// LoginLogItem is one authentication audit row.
type LoginLogItem struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"userId"`
	Username  string    `json:"username"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"userAgent"`
	Success   bool      `json:"success"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"createdAt"`
}

// OperationLogItem is one mutating-request audit row.
type OperationLogItem struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"userId"`
	Username    string    `json:"username"`
	Method      string    `json:"method"`
	Path        string    `json:"path"`
	IP          string    `json:"ip"`
	UserAgent   string    `json:"userAgent"`
	StatusCode  int       `json:"statusCode"`
	LatencyMS   int64     `json:"latencyMs"`
	RequestBody string    `json:"requestBody"`
	CreatedAt   time.Time `json:"createdAt"`
}

// LogQuery filters the audit lists.
type LogQuery struct {
	Keyword  string
	UserID   int64
	Success  *bool
	From     *time.Time
	To       *time.Time
	Page     int
	PageSize int
}

// LogService reads the audit trails written by the authentication flow and the
// operation middleware.
type LogService struct {
	logs *repository.LogRepository
}

// NewLogService wires the audit queries.
func NewLogService(logs *repository.LogRepository) *LogService {
	return &LogService{logs: logs}
}

// ListLogin returns one page of login attempts.
func (s *LogService) ListLogin(ctx context.Context, query LogQuery) ([]LoginLogItem, int64, error) {
	entries, total, err := s.logs.ListLogin(ctx, repository.LogFilter{
		Keyword: query.Keyword,
		UserID:  query.UserID,
		Success: query.Success,
		From:    query.From,
		To:      query.To,
		Page:    repository.Page{Offset: (query.Page - 1) * query.PageSize, Limit: query.PageSize},
	})
	if err != nil {
		return nil, 0, err
	}

	items := make([]LoginLogItem, 0, len(entries))
	for _, entry := range entries {
		items = append(items, LoginLogItem{
			ID:        entry.ID,
			UserID:    entry.UserID,
			Username:  entry.Username,
			IP:        entry.IP,
			UserAgent: entry.UserAgent,
			Success:   entry.Success,
			Message:   entry.Message,
			CreatedAt: entry.CreatedAt,
		})
	}
	return items, total, nil
}

// ListOperation returns one page of mutating API calls.
func (s *LogService) ListOperation(ctx context.Context, query LogQuery) ([]OperationLogItem, int64, error) {
	entries, total, err := s.logs.ListOperation(ctx, repository.LogFilter{
		Keyword: query.Keyword,
		UserID:  query.UserID,
		Success: query.Success,
		From:    query.From,
		To:      query.To,
		Page:    repository.Page{Offset: (query.Page - 1) * query.PageSize, Limit: query.PageSize},
	})
	if err != nil {
		return nil, 0, err
	}

	items := make([]OperationLogItem, 0, len(entries))
	for _, entry := range entries {
		items = append(items, OperationLogItem{
			ID:          entry.ID,
			UserID:      entry.UserID,
			Username:    entry.Username,
			Method:      entry.Method,
			Path:        entry.Path,
			IP:          entry.IP,
			UserAgent:   entry.UserAgent,
			StatusCode:  entry.StatusCode,
			LatencyMS:   entry.LatencyMS,
			RequestBody: entry.RequestBody,
			CreatedAt:   entry.CreatedAt,
		})
	}
	return items, total, nil
}
