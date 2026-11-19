package response

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"github.com/nightelforc/base-admin-core/backend/pkg/apperr"
)

// BindJSON 解码并校验 JSON 请求体，把绑定与校验失败翻译成面向用户的业务错误。
func BindJSON(c *gin.Context, target any) error {
	if err := c.ShouldBindJSON(target); err != nil {
		return translateBindError(err)
	}
	return nil
}

// BindQuery 解码并校验 query-string 参数。
func BindQuery(c *gin.Context, target any) error {
	if err := c.ShouldBindQuery(target); err != nil {
		return translateBindError(err)
	}
	return nil
}

func translateBindError(err error) error {
	if errors.Is(err, io.EOF) {
		return apperr.ErrInvalidParams.WithMessage("请求体不能为空")
	}

	var validationErrors validator.ValidationErrors
	if errors.As(err, &validationErrors) {
		return apperr.ErrInvalidParams.WithMessage("%s", describeValidationErrors(validationErrors))
	}

	return apperr.ErrInvalidParams.WithMessage("请求参数格式不正确")
}

func describeValidationErrors(errs validator.ValidationErrors) string {
	parts := make([]string, 0, len(errs))
	for _, fieldErr := range errs {
		parts = append(parts, describeFieldError(fieldErr))
	}
	return strings.Join(parts, "；")
}

func describeFieldError(fieldErr validator.FieldError) string {
	name := fieldErr.Field()
	if jsonName := jsonFieldName(fieldErr.StructNamespace()); jsonName != "" {
		name = jsonName
	}

	switch fieldErr.Tag() {
	case "required":
		return fmt.Sprintf("%s 不能为空", name)
	case "min":
		if n, err := strconv.Atoi(fieldErr.Param()); err == nil {
			return fmt.Sprintf("%s 不能小于 %d", name, n)
		}
	case "max":
		if n, err := strconv.Atoi(fieldErr.Param()); err == nil {
			return fmt.Sprintf("%s 不能大于 %d", name, n)
		}
	case "gte":
		return fmt.Sprintf("%s 不能小于 %s", name, fieldErr.Param())
	case "lte":
		return fmt.Sprintf("%s 不能大于 %s", name, fieldErr.Param())
	case "len":
		return fmt.Sprintf("%s 长度必须为 %s", name, fieldErr.Param())
	case "email":
		return fmt.Sprintf("%s 不是合法的邮箱地址", name)
	case "url":
		return fmt.Sprintf("%s 不是合法的链接", name)
	case "oneof":
		return fmt.Sprintf("%s 取值必须是 [%s] 之一", name, fieldErr.Param())
	case "e164":
		return fmt.Sprintf("%s 不是合法的手机号", name)
	default:
		return fmt.Sprintf("%s 不满足校验规则 %s", name, fieldErr.Tag())
	}
	return fmt.Sprintf("%s 不满足校验规则 %s", name, fieldErr.Tag())
}

// jsonFieldName 从结构体字段的命名空间路径（例如 "CreateUserRequest.Username"）
// 解析其 json tag；解析不到时回落到原始字段名。
func jsonFieldName(namespace string) string {
	parts := strings.Split(namespace, ".")
	return parts[len(parts)-1]
}
