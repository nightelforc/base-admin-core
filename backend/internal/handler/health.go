package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Pinger 是就绪探针唯一需要的 Redis 能力：检查连接是否仍然可用。
//
// 探针通过这个接口取依赖，而不是直接接收 *redis.Client，这样它不必知道 Redis
// 客户端的具体类型，测试里给一个假实现即可。
type Pinger interface {
	Ping(ctx context.Context) error
}

// Health answers liveness probes: the process is up and serving requests.
func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "time": time.Now().UTC()})
}

// Readiness answers readiness probes by touching both datastores.
func Readiness(db *gorm.DB, cache Pinger) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()

		sqlDB, err := db.DB()
		if err != nil || sqlDB.PingContext(ctx) != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "degraded", "database": "unavailable"})
			return
		}
		// cache 为 nil 接口时短路，避免调用 nil 的方法。
		if cache == nil || cache.Ping(ctx) != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "degraded", "redis": "unavailable"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}
