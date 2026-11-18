package server

import (
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/nightelforc/base-admin-core/backend/internal/config"
)

func TestGinMode(t *testing.T) {
	cases := map[string]string{
		config.EnvDevelopment: gin.DebugMode,
		config.EnvTest:        gin.TestMode,
		config.EnvProduction:  gin.ReleaseMode,
	}
	for env, want := range cases {
		if got := ginMode(env); got != want {
			t.Errorf("ginMode(%q) = %q, want %q", env, got, want)
		}
	}
}
