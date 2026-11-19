// Package apperr 定义应用错误模型。
//
// 到达 HTTP 层的每个错误都是 *Error，同时携带业务码（经响应信封返回给客户端）
// 和应使用的 HTTP 状态。这样处理器不必自行编排状态码，响应层也能统一渲染错误。
package apperr

import (
	"errors"
	"fmt"
	"net/http"
)

// Error 是附带 HTTP 状态的业务错误。
type Error struct {
	Code       int    `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"-"`
	// Cause 是被包装的技术错误。它会被写入日志，永不序列化。
	Cause error `json:"-"`
}

// Error 实现 error 接口。
func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

// Unwrap 把被包装的原因暴露给 errors.Is / errors.As。
func (e *Error) Unwrap() error { return e.Cause }

// Wrap 返回附带技术原因的错误副本。
func (e *Error) Wrap(cause error) *Error {
	clone := *e
	clone.Cause = cause
	return &clone
}

// WithMessage 返回携带面向用户消息的错误副本。
func (e *Error) WithMessage(format string, args ...any) *Error {
	clone := *e
	clone.Message = fmt.Sprintf(format, args...)
	return &clone
}

// New 构建一个业务错误。
func New(code, httpStatus int, message string) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: httpStatus}
}

// From 把任意错误转换为 *Error，默认当作内部服务器错误，避免意外失败泄漏实现细节。
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

// Is 报告 err 是否就是（或包装了）目标业务错误，按业务码匹配，因此另行构造的副本仍能比较相等。
func Is(err error, target *Error) bool {
	var appErr *Error
	if !errors.As(err, &appErr) {
		return false
	}
	return appErr.Code == target.Code
}

// 业务错误码。前两位与 HTTP 状态对应，其余位标识具体失败。
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
	CodeRequestTimeout  = 50400
)

var (
	// ErrInvalidParams 表示输入格式错误或语义不合法。
	ErrInvalidParams = New(CodeInvalidParams, http.StatusBadRequest, "请求参数不合法")
	// ErrUnauthorized 表示凭证缺失或不可用。
	ErrUnauthorized = New(CodeUnauthorized, http.StatusUnauthorized, "登录状态已失效，请重新登录")
	// ErrTokenInvalid 表示令牌无法通过验证。
	ErrTokenInvalid = New(CodeTokenInvalid, http.StatusUnauthorized, "凭证无效")
	// ErrTokenExpired 表示令牌已过期。
	ErrTokenExpired = New(CodeTokenExpired, http.StatusUnauthorized, "凭证已过期")
	// ErrForbidden 表示已认证的主体没有权限。
	ErrForbidden = New(CodeForbidden, http.StatusForbidden, "没有访问权限")
	// ErrAccountDisabled 表示账号已禁用。
	ErrAccountDisabled = New(CodeAccountDisabled, http.StatusForbidden, "账号已被禁用，请联系管理员")
	// ErrNotFound 表示资源不存在。
	ErrNotFound = New(CodeNotFound, http.StatusNotFound, "资源不存在")
	// ErrConflict 表示唯一性冲突或状态冲突。
	ErrConflict = New(CodeConflict, http.StatusConflict, "资源冲突")
	// ErrTooManyAttempts 表示被限流。
	ErrTooManyAttempts = New(CodeTooManyAttempts, http.StatusTooManyRequests, "操作过于频繁，请稍后再试")
	// ErrInternal 表示意外的服务器故障。
	ErrInternal = New(CodeInternal, http.StatusInternalServerError, "服务器内部错误")
	// ErrRequestTimeout 表示请求超出了处理时限。
	// 504 而不是 503：客户端可以照常重试，语义上是「上游没在时限内完成」。
	ErrRequestTimeout = New(CodeRequestTimeout, http.StatusGatewayTimeout, "请求处理超时")

	// ErrUserNotFound 是专门用于账号的未找到错误。
	ErrUserNotFound = New(40401, http.StatusNotFound, "用户不存在")
	// ErrRoleNotFound 是专门用于角色的未找到错误。
	ErrRoleNotFound = New(40402, http.StatusNotFound, "角色不存在")
	// ErrMenuNotFound 是专门用于菜单的未找到错误。
	ErrMenuNotFound = New(40403, http.StatusNotFound, "菜单不存在")
	// ErrUsernameTaken 表示用户名重复。
	ErrUsernameTaken = New(40901, http.StatusConflict, "用户名已存在")
	// ErrRoleCodeTaken 表示角色标识重复。
	ErrRoleCodeTaken = New(40902, http.StatusConflict, "角色标识已存在")
	// ErrMenuNameTaken 表示菜单名称重复。
	ErrMenuNameTaken = New(40903, http.StatusConflict, "菜单名称标识已存在")
	// ErrBadCredentials 对未知用户与错误密码故意保持一致，使该接口不能被用来枚举账号。
	ErrBadCredentials = New(40010, http.StatusBadRequest, "用户名或密码错误")
	// ErrPasswordMismatch 表示修改密码时当前密码错误。
	ErrPasswordMismatch = New(40011, http.StatusBadRequest, "当前密码不正确")
	// ErrPasswordTooWeak 表示密码不符合已配置的策略。
	ErrPasswordTooWeak = New(40012, http.StatusBadRequest, "密码强度不足")
	// ErrSamePassword 表示一次没有实际变更的密码修改。
	ErrSamePassword = New(40013, http.StatusBadRequest, "新密码不能与当前密码相同")
	// ErrLoginBlocked 表示按账号/IP 的登录限流。
	ErrLoginBlocked = New(42901, http.StatusTooManyRequests, "登录失败次数过多，请稍后再试")
	// ErrUserDisabledByAdmin 表示目标账号已被禁用。
	ErrUserDisabledByAdmin = New(40302, http.StatusForbidden, "该账号已被禁用")
	// ErrCannotDeleteSelf 表示尝试删除自身。
	ErrCannotDeleteSelf = New(40904, http.StatusConflict, "不能删除当前登录账号")
	// ErrCannotDisableSelf 表示尝试禁用自身。
	ErrCannotDisableSelf = New(40905, http.StatusConflict, "不能禁用当前登录账号")
	// ErrProtectedRole 表示修改内置角色。
	ErrProtectedRole = New(40906, http.StatusConflict, "内置角色不允许该操作")
	// ErrProtectedMenu 表示删除拥有权限的菜单。
	ErrProtectedMenu = New(40907, http.StatusConflict, "该菜单下存在子菜单，请先删除子菜单")
	// ErrMenuHasChildren 表示尝试删除非叶子节点。
	ErrMenuHasChildren = ErrProtectedMenu
	// ErrRoleInUse 表示角色仍分配给用户或菜单。
	ErrRoleInUse = New(40908, http.StatusConflict, "该角色已被使用，无法删除")
	// ErrUsernameImmutable 表示尝试重命名登录账号。
	ErrUsernameImmutable = New(40014, http.StatusBadRequest, "用户名不允许修改")
)
