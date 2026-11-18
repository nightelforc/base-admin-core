package response

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/nightelforc/base-admin-core/backend/pkg/apperr"
)

// 下游因请求上下文到期而失败时，必须输出 504 / 50400 而不是 500。
func TestFailMapsDeadlineExceededToGatewayTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)

	Fail(c, fmt.Errorf("query users: %w", context.DeadlineExceeded))

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusGatewayTimeout)
	}

	var envelope Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Code != apperr.CodeRequestTimeout {
		t.Fatalf("code = %d, want %d", envelope.Code, apperr.CodeRequestTimeout)
	}
	if envelope.Message != apperr.ErrRequestTimeout.Message {
		t.Fatalf("message = %q, want %q", envelope.Message, apperr.ErrRequestTimeout.Message)
	}
}

// 普通业务错误不受影响，仍按自带的业务码与状态码输出。
func TestFailKeepsBusinessError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)

	Fail(c, apperr.ErrUserNotFound)

	if rec.Code != apperr.ErrUserNotFound.HTTPStatus {
		t.Fatalf("status = %d, want %d", rec.Code, apperr.ErrUserNotFound.HTTPStatus)
	}

	var envelope Envelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Code != apperr.ErrUserNotFound.Code {
		t.Fatalf("code = %d, want %d", envelope.Code, apperr.ErrUserNotFound.Code)
	}
}
