package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Envelope：全项目唯一的响应信封，必须与 Node 版 src/utils/response.js 完全一致：
//
//	成功 { code:0, message:"ok", data:... }   HTTP 200
//	失败 { code:<status>, message:"...", data:null }  HTTP status == code
type Envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, Envelope{Code: 0, Message: "ok", Data: data})
}

func Fail(c *gin.Context, status int, message string) {
	c.JSON(status, Envelope{Code: status, Message: message, Data: nil})
}

// ApiError：业务异常。handler 只要 return Err(404, "商品不存在")，
// Wrap 会统一转成上面那个失败信封，避免每个 handler 散写响应。
type ApiError struct {
	Status  int
	Message string
}

func (e *ApiError) Error() string { return e.Message }

func Err(status int, message string) error {
	return &ApiError{Status: status, Message: message}
}

// Handler：带 error 返回的 handler 类型。
type Handler func(*gin.Context) error

// Wrap：把 error 统一映射成响应。这样所有 handler 都只有一条「成功出口」+ 若干「错误 return」。
func Wrap(h Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := h(c); err != nil {
			if ae, ok := err.(*ApiError); ok {
				Fail(c, ae.Status, ae.Message)
			} else {
				Fail(c, http.StatusInternalServerError, err.Error())
			}
		}
	}
}
