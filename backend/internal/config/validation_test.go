package config

import (
	"strings"
	"testing"
)

// TestValidateAggregatesAllProblemKinds 证明校验不再"报一个就返回"：schema 错误、
// 组件跨字段错误、全局超时约束各错一处时，三处问题要出现在同一条错误里。
func TestValidateAggregatesAllProblemKinds(t *testing.T) {
	path := writeConfig(t, `
log:
  level: verbose
security:
  bcrypt_cost: 2
http:
  request_timeout: 10s
  max_body_bytes: 0
database:
  statement_timeout: 30s
`)
	t.Setenv(ConfigFileEnv, path)
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))

	_, err := Load()
	if err == nil {
		t.Fatal("Load succeeded, want aggregated validation error")
	}

	for _, want := range []string{
		"LOG_LEVEL must be one of: debug, info, warn, error",             // schema: oneof
		"SECURITY_BCRYPT_COST must be at least 4",                        // schema: min
		"HTTP_MAX_BODY_BYTES must be positive",                           // 组件内校验
		"DB_STATEMENT_TIMEOUT must be shorter than HTTP_REQUEST_TIMEOUT", // 跨组件超时约束
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Load error = %v, want it to mention %q", err, want)
		}
	}

	// 报错必须指回环境变量名；validator 自带的命名空间和 Go 字段名都不能拿来搜。
	for _, unwanted := range []string{"Config.Log.Level", "Field validation for"} {
		if strings.Contains(err.Error(), unwanted) {
			t.Errorf("Load error still leaks validator internals (%q): %v", unwanted, err)
		}
	}
	if !strings.Contains(err.Error(), "invalid configuration") {
		t.Errorf("Load error = %v, want the shared error header", err)
	}
}

// TestDescribeSchemaField 钉住 tag → 文案的映射；新增 tag 时这里会提示补翻译。
func TestDescribeSchemaField(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		want  string
	}{
		{
			name: "required",
			value: struct {
				Env string `env:"ENV" validate:"required"`
			}{},
			want: "APP_ENV is required",
		},
		{
			name: "oneof",
			value: struct {
				Level string `env:"LEVEL" validate:"oneof=debug info"`
			}{Level: "trace"},
			want: "APP_LEVEL must be one of: debug, info",
		},
		{
			name: "min on number",
			value: struct {
				Cost int `env:"COST" validate:"min=4"`
			}{Cost: 1},
			want: "APP_COST must be at least 4",
		},
		{
			name: "min on string",
			value: struct {
				Secret string `env:"SECRET" validate:"min=32"`
			}{Secret: "short"},
			want: "APP_SECRET must be at least 32 characters",
		},
		{
			// 没有 env tag 时退回 Go 字段名，至少还能定位。
			name: "without env tag",
			value: struct {
				Raw string `validate:"required"`
			}{},
			want: "APP_Raw is required",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := describeSchemaProblems("APP_", tc.value).err()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("describeSchemaProblems() = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}
