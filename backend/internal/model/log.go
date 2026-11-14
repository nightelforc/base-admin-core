package model

import "time"

// LoginLog records one authentication attempt, successful or not.
type LoginLog struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	UserID    int64     `gorm:"column:user_id;not null;default:0" json:"userId"`
	Username  string    `gorm:"column:username;size:64;not null;default:''" json:"username"`
	IP        string    `gorm:"column:ip;size:45;not null;default:''" json:"ip"`
	UserAgent string    `gorm:"column:user_agent;size:512;not null;default:''" json:"userAgent"`
	Success   bool      `gorm:"column:success;not null;default:false" json:"success"`
	Message   string    `gorm:"column:message;size:255;not null;default:''" json:"message"`
	CreatedAt time.Time `gorm:"column:created_at;not null" json:"createdAt"`
}

// OperationLog records one mutating API call.
type OperationLog struct {
	ID          int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	UserID      int64     `gorm:"column:user_id;not null;default:0" json:"userId"`
	Username    string    `gorm:"column:username;size:64;not null;default:''" json:"username"`
	Method      string    `gorm:"column:method;size:16;not null;default:''" json:"method"`
	Path        string    `gorm:"column:path;size:255;not null;default:''" json:"path"`
	IP          string    `gorm:"column:ip;size:45;not null;default:''" json:"ip"`
	UserAgent   string    `gorm:"column:user_agent;size:512;not null;default:''" json:"userAgent"`
	StatusCode  int       `gorm:"column:status_code;not null;default:0" json:"statusCode"`
	LatencyMS   int64     `gorm:"column:latency_ms;not null;default:0" json:"latencyMs"`
	RequestBody string    `gorm:"column:request_body;size:4096;not null;default:''" json:"requestBody"`
	CreatedAt   time.Time `gorm:"column:created_at;not null" json:"createdAt"`
}
