package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// 图片 URL 契约（与 Node 版 src/utils/assetUrl.js、PHP 版 App\Support\Api 完全一致）：
//
//	数据库只存相对路径 /uploads/xxx.png；接口一律返回完整 URL。
//	前端跨域部署时不必再靠 vite proxy 转发 /uploads。
//	origin 优先级：PUBLIC_ORIGIN（生产 / CDN / 反代必配） > 当前请求的 scheme://host
//
// 约定：业务字段里凡是 /uploads/ 开头的字符串，都视为本系统上传的图片路径。

// assetOrigin 由 NewRouter 在启动时从 config 注入，之后只读。
var assetOrigin string

func setAssetOrigin(v string) {
	assetOrigin = strings.TrimRight(strings.TrimSpace(v), "/")
}

// uploadAbsRe 匹配 http(s)://host/uploads/... 与 //host/uploads/...，保留 query / hash
var uploadAbsRe = regexp.MustCompile(`(?i)^(?:https?:)?//[^/]+(/uploads/[^?#]*)([?#].*)?$`)

func publicOrigin(c *gin.Context) string {
	if assetOrigin != "" {
		return assetOrigin
	}
	if c == nil || c.Request == nil || c.Request.Host == "" {
		return ""
	}
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + c.Request.Host
}

func toPublicURL(s, origin string) string {
	if origin == "" || !strings.HasPrefix(s, "/uploads/") {
		return s
	}
	return origin + s
}

func toStoragePath(s string) string {
	if m := uploadAbsRe.FindStringSubmatch(s); m != nil {
		return m[1] + m[2]
	}
	return s
}

// walkAsset 递归遍历响应 / 请求体，返回 (处理后的值, 是否有改动)。
// 用反射是为了同时兼容 map[string]any、gin.H、[]map[string]any、[]any 等形状。
func walkAsset(v any, fn func(string) string, depth int) (any, bool) {
	if v == nil || depth > 8 {
		return v, false
	}
	switch x := v.(type) {
	case string:
		out := fn(x)
		return out, out != x
	case time.Time, []byte:
		return x, false
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map:
		out := make(map[string]any, rv.Len())
		changed := false
		iter := rv.MapRange()
		for iter.Next() {
			val, ch := walkAsset(iter.Value().Interface(), fn, depth+1)
			out[fmt.Sprint(iter.Key().Interface())] = val
			changed = changed || ch
		}
		if !changed {
			return v, false
		}
		return out, true
	case reflect.Slice, reflect.Array:
		out := make([]any, rv.Len())
		changed := false
		for i := 0; i < rv.Len(); i++ {
			val, ch := walkAsset(rv.Index(i).Interface(), fn, depth+1)
			out[i] = val
			changed = changed || ch
		}
		if !changed {
			return v, false
		}
		return out, true
	case reflect.Ptr, reflect.Interface:
		if rv.IsNil() {
			return v, false
		}
		val, ch := walkAsset(rv.Elem().Interface(), fn, depth+1)
		if !ch {
			return v, false
		}
		return val, true
	default:
		return v, false
	}
}

// absolutize：响应出口把 /uploads/ 相对路径补成完整 URL
func absolutize(v any, origin string) any {
	if origin == "" {
		return v
	}
	out, _ := walkAsset(v, func(s string) string { return toPublicURL(s, origin) }, 0)
	return out
}

// containsUploadFold 做零分配的 ASCII 大小写不敏感 "uploads" 扫描。
// 与 uploadAbsRe 的 (?i) 语义对齐：/UPLOADS/a.png 之类写法也能命中，不会被快速路径漏掉。
func containsUploadFold(b []byte) bool {
	const marker = "uploads"
	for i := 0; i+len(marker) <= len(b); i++ {
		hit := true
		for j := 0; j < len(marker); j++ {
			ch := b[i+j]
			if ch >= 'A' && ch <= 'Z' {
				ch += 'a' - 'A'
			}
			if ch != marker[j] {
				hit = false
				break
			}
		}
		if hit {
			return true
		}
	}
	return false
}

// NormalizeAssetBody：只处理 JSON 请求体。若没有图片完整 URL，就把原始字节原封不动放回去，
// 避免把普通请求体 round-trip 一遍（数字会被解成 float64）造成类型变化。
func NormalizeAssetBody() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body == nil || !strings.Contains(strings.ToLower(c.ContentType()), "application/json") {
			c.Next()
			return
		}
		raw, err := io.ReadAll(c.Request.Body)
		_ = c.Request.Body.Close()
		if err != nil {
			c.Next()
			return
		}
		// 快速路径：body 里连 "uploads" 字样都没有，直接把原始字节放回去，
		// 省掉一次完整的 json.Unmarshal（登录 / 分页 / 状态变更等绝大多数请求走这里）。
		// ① 用 containsUploadFold 与正则 (?i) 对齐，避免 /UPLOADS/ 被漏判；
		// ② JSON 允许把字符写成 \uXXXX，含反斜杠时不敢断言没有 uploads，老实走完整解析。
		if bytes.IndexByte(raw, '\\') < 0 && !containsUploadFold(raw) {
			c.Request.Body = io.NopCloser(bytes.NewReader(raw))
			c.Next()
			return
		}
		var parsed any
		if json.Unmarshal(raw, &parsed) != nil {
			c.Request.Body = io.NopCloser(bytes.NewReader(raw))
			c.Next()
			return
		}
		cleaned, changed := walkAsset(parsed, toStoragePath, 0)
		// 只有真正发生替换时才重新序列化
		if !changed {
			c.Request.Body = io.NopCloser(bytes.NewReader(raw))
			c.Next()
			return
		}
		out, err := json.Marshal(cleaned)
		if err != nil {
			out = raw
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(out))
		c.Request.ContentLength = int64(len(out))
		c.Next()
	}
}
