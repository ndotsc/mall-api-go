package httpapi

import (
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jmoiron/sqlx"

	"mall-api-go/internal/config"
	"mall-api-go/internal/service"
)

// Deps：所有 handler 共享的依赖（对应 Node 版各个 import 进来的单例）。
type Deps struct {
	DB  *sqlx.DB
	JWT *service.JWTService
	Cfg config.Config
}

// ---------- 鉴权 ----------

// Auth：解析 Bearer token，把 userId/role/jti/exp/familyId 写进 Context。
// 与 Node 版 middleware/auth.js 的 authRequired 一致。
func Auth(j *service.JWTService) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") || len(header) <= 7 {
			c.AbortWithStatusJSON(401, Envelope{Code: 401, Message: "未登录或登录已过期", Data: nil})
			return
		}
		claims, err := j.VerifyAccess(header[7:])
		if err != nil {
			c.AbortWithStatusJSON(401, Envelope{Code: 401, Message: err.Error(), Data: nil})
			return
		}
		c.Set("userId", service.ClaimInt(claims, "userId"))
		c.Set("role", service.ClaimInt(claims, "role"))
		c.Set("jti", service.ClaimString(claims, "jti"))
		c.Set("familyId", service.ClaimString(claims, "familyId"))
		c.Set("exp", int64(service.ClaimFloat(claims, "exp")))
		c.Next()
	}
}

// Admin：role === 2 才放行（对应 adminRequired）
func Admin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetInt("role") != 2 {
			c.AbortWithStatusJSON(403, Envelope{Code: 403, Message: "需要管理员权限", Data: nil})
			return
		}
		c.Next()
	}
}

// ---------- 限流：同 IP 15 分钟 10 次（对应 auth.js 的 authLimiter）----------

type rateState struct {
	mu    sync.Mutex
	hits  map[string][]time.Time
	limit int
	win   time.Duration
}

var limiter = &rateState{hits: make(map[string][]time.Time), limit: 10, win: 15 * time.Minute}

func RateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 方便跑集成测试：APP_ENV=test 时跳过（Node 版用 NODE_ENV=test）
		if os.Getenv("APP_ENV") == "test" {
			c.Next()
			return
		}
		ip := c.ClientIP()
		now := time.Now()
		limiter.mu.Lock()
		recent := limiter.hits[ip][:0]
		for _, t := range limiter.hits[ip] {
			if now.Sub(t) < limiter.win {
				recent = append(recent, t)
			}
		}
		if len(recent) >= limiter.limit {
			limiter.hits[ip] = recent
			limiter.mu.Unlock()
			c.AbortWithStatusJSON(429, Envelope{Code: 429, Message: "尝试次数过多，请 15 分钟后再试", Data: nil})
			return
		}
		limiter.hits[ip] = append(recent, now)
		limiter.mu.Unlock()
		c.Next()
	}
}

// ---------- CORS / 请求体限制 / 错误兜底 ----------

func CORS(origins string) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := origins
		if origins != "*" && strings.Contains(origins, ",") {
			// 多域名时按请求头匹配
			reqOrigin := c.GetHeader("Origin")
			for _, o := range strings.Split(origins, ",") {
				if strings.TrimSpace(o) == reqOrigin {
					origin = reqOrigin
					break
				}
			}
		}
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Authorization,Content-Type")
		c.Header("Access-Control-Max-Age", "86400")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// BodyLimit：1MB（对应 Node 版 app.js 的 express.json({limit:'1mb'})）
func BodyLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		c.Next()
	}
}

// Recovery：panic 也转成统一信封，不让 gin 默认输出纯文本
func Recovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, _ any) {
		Fail(c, 500, "服务器内部错误")
	})
}

// NoRoute：404 兜底，消息与 Node 版 error.js 的 notFound 一致
func NoRoute(c *gin.Context) {
	Fail(c, 404, "接口不存在: "+c.Request.Method+" "+c.Request.URL.RequestURI())
}
