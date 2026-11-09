package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

const testOrigin = "http://localhost:3002"

func TestToPublicURL(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"相对路径补 origin", "/uploads/a.png", testOrigin + "/uploads/a.png"},
		{"外链原样", "https://picsum.photos/seed/p1/400/400", "https://picsum.photos/seed/p1/400/400"},
		{"已是完整 URL 原样", testOrigin + "/uploads/a.png", testOrigin + "/uploads/a.png"},
		{"data 原样", "data:image/png;base64,AAA", "data:image/png;base64,AAA"},
	}
	for _, c := range cases {
		if got := toPublicURL(c.in, testOrigin); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestToStoragePath(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"http", "http://localhost:3002/uploads/a.png", "/uploads/a.png"},
		{"https + query", "https://cdn.example.com/uploads/a.png?v=1", "/uploads/a.png?v=1"},
		{"协议相对", "//localhost:3002/uploads/a.png", "/uploads/a.png"},
		{"相对路径幂等", "/uploads/a.png", "/uploads/a.png"},
		{"外链原样", "https://picsum.photos/seed/p1/400/400", "https://picsum.photos/seed/p1/400/400"},
	}
	for _, c := range cases {
		if got := toStoragePath(c.in); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestAbsolutizeNested(t *testing.T) {
	data := map[string]any{
		"list": []map[string]any{{"cover": "/uploads/a.png", "name": "商品"}},
		"url":  "/uploads/b.png",
	}
	out := absolutize(data, testOrigin).(map[string]any)
	list := out["list"].([]any)
	row := list[0].(map[string]any)
	if row["cover"] != testOrigin+"/uploads/a.png" {
		t.Fatalf("嵌套 cover 未补全: %v", row["cover"])
	}
	if out["url"] != testOrigin+"/uploads/b.png" {
		t.Fatalf("url 未补全: %v", out["url"])
	}
}

// 走真实响应出口 OK()：确认按请求 Host 推导 origin 后，cover 变成完整 URL。
func TestOKReturnsAbsoluteCover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	setAssetOrigin("")
	defer setAssetOrigin("")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "http://localhost:3002/api/products", nil)

	OK(c, map[string]any{"cover": "/uploads/a.png", "price": 9900})

	var env Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("data 形状异常: %#v", env.Data)
	}
	if data["cover"] != testOrigin+"/uploads/a.png" {
		t.Fatalf("cover 未补成完整 URL: %v", data["cover"])
	}
	if env.Code != 0 || env.Message != "ok" {
		t.Fatalf("响应信封被破坏: %+v", env)
	}
}

func TestNormalizeAssetBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(BodyLimit(), NormalizeAssetBody())
	r.POST("/", func(c *gin.Context) {
		raw, _ := io.ReadAll(c.Request.Body)
		c.Data(http.StatusOK, "application/json", raw)
	})

	// 1) 有图片完整 URL：还原成相对路径，其他字段不变
	body := `{"cover":"http://localhost:3002/uploads/a.png","price":9900}`
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("响应不是合法 JSON: %v, body=%s", err, w.Body.String())
	}
	if got["cover"] != "/uploads/a.png" {
		t.Fatalf("cover 未还原: %v", got["cover"])
	}
	if got["price"] != float64(9900) {
		t.Fatalf("price 被改动: %v", got["price"])
	}

	// 2) 没有图片 URL：原始字节应原封不动，不做 round-trip
	plain := `{"name":"  保留空格  ","price":1.50,"ok":true}`
	req2 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(plain))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	if w2.Body.String() != plain {
		t.Fatalf("普通请求体被改写:\n got  %s\n want %s", w2.Body.String(), plain)
	}

	// 3) 大小写变体不能被快速路径漏判（正则带 (?i)，捕获组保留原大小写）
	upper := `{"cover":"http://localhost:3002/UPLOADS/a.png"}`
	req3 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(upper))
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)
	var got3 map[string]any
	if err := json.Unmarshal(w3.Body.Bytes(), &got3); err != nil {
		t.Fatalf("大小写变体响应不是合法 JSON: %v", err)
	}
	if got3["cover"] != "/UPLOADS/a.png" {
		t.Fatalf("大小写变体未被还原: %v", got3["cover"])
	}

	// 4) JSON 里把 / 写成 \/ ：含反斜杠时走完整解析，仍需还原
	escaped := `{"cover":"http:\/\/localhost:3002\/uploads\/a.png"}`
	req4 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(escaped))
	req4.Header.Set("Content-Type", "application/json")
	w4 := httptest.NewRecorder()
	r.ServeHTTP(w4, req4)
	var got4 map[string]any
	if err := json.Unmarshal(w4.Body.Bytes(), &got4); err != nil {
		t.Fatalf("转义斜杠响应不是合法 JSON: %v", err)
	}
	if got4["cover"] != "/uploads/a.png" {
		t.Fatalf("转义斜杠未被还原: %v", got4["cover"])
	}
}

func TestContainsUploadFold(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{`{"name":"商品","price":9900}`, false},
		{`{"cover":"http://localhost:3002/uploads/a.png"}`, true},
		{`{"cover":"HTTP://CDN/UPLOADS/A.PNG"}`, true},
		{`{"cover":"\u002fuploads\u002fa.png"}`, true},
	}
	for _, c := range cases {
		if got := containsUploadFold([]byte(c.in)); got != c.want {
			t.Errorf("containsUploadFold(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
