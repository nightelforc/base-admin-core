// Package apperr defines the application error model.
//
// Every error that reaches the HTTP layer is an *Error carrying both a business
// code (returned to the client in the response envelope) and the HTTP status to
// use. This keeps handlers free of status-code juggling and lets the response
// layer render errors uniformly.
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

// Error is a business error with an HTTP status attached.
type Error struct {
	Code       int    `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"-"`
	// Cause is the wrapped technical error. It is logged, never serialised.
	Cause error `json:"-"`
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

// Unwrap exposes the wrapped cause to errors.Is / errors.As.
func (e *Error) Unwrap() error { return e.Cause }

// Wrap returns a copy of the error with the technical cause attached.
func (e *Error) Wrap(cause error) *Error {
	clone := *e
	clone.Cause = cause
	return &clone
}

// WithMessage returns a copy of the error carrying a user-facing message.
func (e *Error) WithMessage(format string, args ...any) *Error {
	clone := *e
	clone.Message = fmt.Sprintf(format, args...)
	return &clone
}

// New builds a business error.
func New(code, httpStatus int, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: httpStatus}
}

// From converts an arbitrary error into an *Error, defaulting to an internal
// server error so unexpected failures never leak implementation details.
func From(err error) *Error {
	if err == nil {
		return nil
	}
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr
	}
	return ErrInternal.Wrap(err)
}

// Is reports whether err is (or wraps) the target business error, matching on
// the business code so that ad-hoc copies still compare equal.
func Is(err error, target *Error) bool {
	var appErr *Error
	if !errors.As(err, &appErr) {
		return false
	}
	return appErr.Code == target.Code
}

// Business error codes. The first two digits mirror the HTTP status, the rest
// identify the concrete failure.
const (
	CodeSuccess         = 0
	CodeInvalidParams   = 40000
	CodeUnauthorized    = 40100
	CodeTokenInvalid    = 40101
	CodeTokenExpired    = 40102
	CodeForbidden       = 40300
	CodeAccountDisabled = 40301
	CodeNotFound        = 40400
	CodeConflict        = 40900
	CodeTooManyAttempts = 42900
	CodeInternal        = 50000
)

var (
	// ErrInvalidParams reports malformed or semantically invalid input.
	ErrInvalidParams = New(CodeInvalidParams, http.StatusBadRequest, "请求参数不合法")
	// ErrUnauthorized reports a missing or unusable credential.
	ErrUnauthorized = New(CodeUnauthorized, http.StatusUnauthorized, "登录状态已失效，请重新登录")
	// ErrTokenInvalid reports a token that cannot be verified.
	ErrTokenInvalid = New(CodeTokenInvalid, http.StatusUnauthorized, "凭证无效")
	// ErrTokenExpired reports an expired token.
	ErrTokenExpired = New(CodeTokenExpired, http.StatusUnauthorized, "凭证已过期")
	// ErrForbidden reports an authenticated principal without permission.
	ErrForbidden = New(CodeForbidden, http.StatusForbidden, "没有访问权限")
	// ErrAccountDisabled reports a disabled account.
	ErrAccountDisabled = New(CodeAccountDisabled, http.StatusForbidden, "账号已被禁用，请联系管理员")
	// ErrNotFound reports a missing resource.
	ErrNotFound = New(CodeNotFound, http.StatusNotFound, "资源不存在")
	// ErrConflict reports a uniqueness or state conflict.
	ErrConflict = New(CodeConflict, http.StatusConflict, "资源冲突")
	// ErrTooManyAttempts reports throttling.
	ErrTooManyAttempts = New(CodeTooManyAttempts, http.StatusTooManyRequests, "操作过于频繁，请稍后再试")
	// ErrInternal reports an unexpected server failure.
	ErrInternal = New(CodeInternal, http.StatusInternalServerError, "服务器内部错误")

	// ErrUserNotFound is a not-found error specialised for accounts.
	ErrUserNotFound = New(40401, http.StatusNotFound, "用户不存在")
	// ErrRoleNotFound is a not-found error specialised for roles.
	ErrRoleNotFound = New(40402, http.StatusNotFound, "角色不存在")
	// ErrMenuNotFound is a not-found error specialised for menus.
	ErrMenuNotFound = New(40403, http.StatusNotFound, "菜单不存在")
	// ErrUsernameTaken reports a duplicate username.
	ErrUsernameTaken = New(40901, http.StatusConflict, "用户名已存在")
	// ErrRoleCodeTaken reports a duplicate role code.
	ErrRoleCodeTaken = New(40902, http.StatusConflict, "角色标识已存在")
	// ErrMenuNameTaken reports a duplicate menu name.
	ErrMenuNameTaken = New(40903, http.StatusConflict, "菜单名称标识已存在")
	// ErrBadCredentials is intentionally identical for unknown users and wrong
	// passwords so the endpoint cannot be used to enumerate accounts.
	ErrBadCredentials = New(40010, http.StatusBadRequest, "用户名或密码错误")
	// ErrPasswordMismatch reports a wrong current password on change-password.
	ErrPasswordMismatch = New(40011, http.StatusBadRequest, "当前密码不正确")
	// ErrPasswordTooWeak reports a password that fails the configured policy.
	ErrPasswordTooWeak = New(40012, http.StatusBadRequest, "密码强度不足")
	// ErrSamePassword reports a no-op password change.
	ErrSamePassword = New(40013, http.StatusBadRequest, "新密码不能与当前密码相同")
	// ErrLoginBlocked reports the per-account/IP login throttle.
	ErrLoginBlocked = New(42901, http.StatusTooManyRequests, "登录失败次数过多，请稍后再试")
	// ErrUserDisabledByAdmin reports a disabled target account.
	ErrUserDisabledByAdmin = New(40302, http.StatusForbidden, "该账号已被禁用")
	// ErrCannotDeleteSelf reports a self-deletion attempt.
	ErrCannotDeleteSelf = New(40904, http.StatusConflict, "不能删除当前登录账号")
	// ErrCannotDisableSelf reports a self-disable attempt.
	ErrCannotDisableSelf = New(40905, http.StatusConflict, "不能禁用当前登录账号")
	// ErrProtectedRole reports modification of a built-in role.
	ErrProtectedRole = New(40906, http.StatusConflict, "内置角色不允许该操作")
	// ErrProtectedMenu reports deletion of a menu that owns permissions.
	ErrProtectedMenu = New(40907, http.StatusConflict, "该菜单下存在子菜单，请先删除子菜单")
	// ErrMenuHasChildren reports a non-leaf deletion attempt.
	ErrMenuHasChildren = ErrProtectedMenu
	// ErrRoleInUse reports a role still assigned to users or menus.
	ErrRoleInUse = New(40908, http.StatusConflict, "该角色已被使用，无法删除")
	// ErrUsernameImmutable reports an attempt to rename the login account.
	ErrUsernameImmutable = New(40014, http.StatusBadRequest, "用户名不允许修改")
)
