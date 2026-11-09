package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Config：全部配置来自 .env（见 .env.example），与 Node 版 mall-api 保持同名，
// 这样三套后端可以共用一份「环境变量心智模型」。
type Config struct {
	DSN            string // 转换后的 go-sql-driver DSN
	JwtSecret      string // 必须与 Node 版 mall-api 完全一致，令牌才能互认
	AccessTTLMin   int
	RefreshTTLDays int
	Port           string
	CORSOrigins    string
	WXAppID        string
	WXSecret       string
	WXOAAppID      string
	WXOASecret     string
	UploadDir      string // 静态图片目录；三套后端共用 ../uploads
	PublicOrigin   string // 对外可访问的 origin，如 https://api.example.com 或 CDN；留空按请求推导
}

func get(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func Load() Config {
	// 加载不到 .env 不报错：生产环境通常直接注入环境变量
	_ = godotenv.Load()

	c := Config{
		JwtSecret:      get("JWT_SECRET", "change-me-in-production"),
		AccessTTLMin:   getInt("JWT_ACCESS_TTL_MIN", 15),
		RefreshTTLDays: getInt("JWT_REFRESH_TTL_DAYS", 7),
		Port:           get("PORT", "3002"),
		CORSOrigins:    get("CORS_ORIGINS", "*"),
		WXAppID:        get("WX_APPID", ""),
		WXSecret:       get("WX_SECRET", ""),
		WXOAAppID:      get("WX_OA_APPID", ""),
		WXOASecret:     get("WX_OA_SECRET", ""),
		UploadDir:      get("UPLOAD_DIR", "public/uploads"),
		PublicOrigin:   get("PUBLIC_ORIGIN", ""),
	}
	c.DSN = buildDSN(get("DATABASE_URL", "mysql://mall:mall123@localhost:3306/mall"))
	return c
}

// Node/Prisma 的 DATABASE_URL 是 URL 形式（mysql://user:pass@host:3306/db），
// 而 go-sql-driver 要的是 "user:pass@tcp(host:3306)/db?params"，这里做一次转换。
// parseTime=true 让 DATETIME 直接扫成 time.Time（JSON 输出即 RFC3339，与 Node 的 ISO 串一致）。
//
// ★ loc 必须是 UTC：三套后端共用一个库，Prisma 把 DateTime 以 UTC 墙钟时间写进 DATETIME，
//
//	PHP 版也用 gmdate 写 UTC。若这里用 loc=Local，同一行 DATETIME 会被当成北京时间解析，
//	createdAt / validTo 整体偏 8 小时，会出现「Go 建的券，Node/PHP 判有效期不一致」。
func buildDSN(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	var auth strings.Builder
	if u.User != nil {
		pw, _ := u.User.Password()
		auth.WriteString(u.User.Username())
		if pw != "" {
			auth.WriteString(":" + pw)
		}
		auth.WriteString("@")
	}
	db := strings.TrimPrefix(u.Path, "/")
	host := u.Host
	if !strings.Contains(host, ":") {
		host += ":3306"
	}
	return fmt.Sprintf("%stcp(%s)/%s?parseTime=true&loc=UTC&charset=utf8mb4", auth.String(), host, db)
}

// 微信凭证：公众号专用优先，未配置则回退小程序凭证（与 Node 版 utils/wechat.js 一致）
func (c Config) OAuthAppID() string {
	if c.WXOAAppID != "" {
		return c.WXOAAppID
	}
	return c.WXAppID
}

func (c Config) OAuthSecret() string {
	if c.WXOASecret != "" {
		return c.WXOASecret
	}
	return c.WXSecret
}
