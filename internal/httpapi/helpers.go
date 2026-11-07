package httpapi

import (
	"time"

	"mall-api-go/internal/service"
)

// 行数据已从 db.normalize 归一：字符串是 string、tinyint 是 bool、DATETIME 是 time.Time。
// 下面几个小工具让 handler 里少写一串类型断言。

func toBool(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return service.ToInt(v) != 0
}

func toInt(v any) int { return service.ToInt(v) }

func toStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func toTime(v any) time.Time {
	if t, ok := v.(time.Time); ok {
		return t
	}
	return time.Time{}
}

// publicUser：绝不把 password / openid 等敏感字段返回给前端
func publicUser(m map[string]any) map[string]any {
	return map[string]any{
		"id":        m["id"],
		"username":  m["username"],
		"nickname":  m["nickname"],
		"role":      m["role"],
		"avatar":    m["avatar"],
		"createdAt": m["createdAt"],
	}
}

// wechatUser：微信登录返回的用户形状（Node 版只给 id/nickname/role/avatar）
func wechatUser(m map[string]any) map[string]any {
	return map[string]any{
		"id":       m["id"],
		"nickname": m["nickname"],
		"role":     m["role"],
		"avatar":   m["avatar"],
	}
}
