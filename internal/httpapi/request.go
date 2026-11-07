package httpapi

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// ---------- 请求体结构（字段名与 Node 版 zod schema / 前端 payload 一致）----------

type registerReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Nickname string `json:"nickname"`
}
type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
type wxCodeReq struct {
	Code string `json:"code"`
}
type refreshReq struct {
	RefreshToken string `json:"refreshToken"`
}
type productReq struct {
	Name       string `json:"name"`
	Price      *int   `json:"price"`
	Cover      string `json:"cover"`
	Desc       string `json:"desc"`
	CategoryID *int   `json:"categoryId"`
}
type cartAddReq struct {
	ProductID int `json:"productId"`
	Number    int `json:"number"`
}
type cartNumberReq struct {
	Number int `json:"number"`
}
type addressReq struct {
	Name      string `json:"name"`
	Phone     string `json:"phone"`
	Detail    string `json:"detail"`
	IsDefault *bool  `json:"isDefault"`
}
type orderItemReq struct {
	ProductID int `json:"productId"`
	Number    int `json:"number"`
}
type receiverInfo struct {
	Name    string `json:"name"`
	Phone   string `json:"phone"`
	Address string `json:"address"`
}
type orderReq struct {
	Items    []orderItemReq `json:"items"`
	Info     receiverInfo   `json:"info"`
	PayType  string         `json:"payType"`
	Remark   string         `json:"remark"`
	CouponID *int           `json:"couponId"`
}
type statusReq struct {
	Status *int `json:"status"`
}
type articleReq struct {
	Title   string `json:"title"`
	Cover   string `json:"cover"`
	Summary string `json:"summary"`
	Content string `json:"content"`
	Status  *int   `json:"status"`
}
type reviewReq struct {
	ProductID int    `json:"productId"`
	Rating    int    `json:"rating"`
	Content   string `json:"content"`
}
type favoriteReq struct {
	ProductID int `json:"productId"`
}
type couponReq struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	Value        *int   `json:"value"`
	ValidFrom    string `json:"validFrom"`
	ValidTo      string `json:"validTo"`
	Threshold    *int   `json:"threshold"`
	MaxDiscount  *int   `json:"maxDiscount"`
	Total        *int   `json:"total"`
	PerUserLimit *int   `json:"perUserLimit"`
}

// ---------- 通用辅助 ----------

func bindJSON(c *gin.Context, dst any) error {
	if err := c.ShouldBindJSON(dst); err != nil {
		return Err(400, "请求参数格式错误")
	}
	return nil
}

// pagination：page 从 1 开始，pageSize 上限 50（与 Node 版一致）
func pagination(c *gin.Context) (page, pageSize int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ = strconv.Atoi(c.DefaultQuery("pageSize", "50"))
	if pageSize < 1 {
		pageSize = 50
	}
	if pageSize > 50 {
		pageSize = 50
	}
	return
}

func uid(c *gin.Context) int        { return c.GetInt("userId") }
func urole(c *gin.Context) int      { return c.GetInt("role") }
func ujti(c *gin.Context) string    { return c.GetString("jti") }
func ufamily(c *gin.Context) string { return c.GetString("familyId") }
func uexp(c *gin.Context) int64     { return c.GetInt64("exp") }

// toMySQLTime：前端传 ISO8601（2026-11-01T00:00:00.000Z），
// MySQL DATETIME 需要 "2006-01-02 15:04:05"，这里做一次转换（与 Prisma 的 Date 落库等价）。
// ★ 必须转 UTC 再格式化：库里的 DATETIME 统一是 UTC 墙钟时间（Prisma / PHP gmdate 都这么写），
//
//	用 t.Local() 会把同一时刻写成本地时间，导致跨后端判断券有效期时差 8 小时。
func toMySQLTime(s string) (string, error) {
	if s == "" {
		return "", Err(400, "时间不能为空")
	}
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format("2006-01-02 15:04:05"), nil
		}
	}
	return "", Err(400, "时间格式不合法，请用 ISO8601")
}

// 允许排序的白名单（非法值一律回退 created_desc，与 Node 版 SORT_MAP 一致）
var productSorts = map[string]string{
	"created_desc": "`createdAt` DESC",
	"created_asc":  "`createdAt` ASC",
	"price_asc":    "`price` ASC",
	"price_desc":   "`price` DESC",
}

func sortClause(c *gin.Context) string {
	if s, ok := productSorts[c.Query("sort")]; ok {
		return s
	}
	return productSorts["created_desc"]
}

// float/nil 安全的字符串取参（空串视为未提供，避免 Number(”)===0 那个经典坑）
func queryTrim(c *gin.Context, key string) string {
	return strings.TrimSpace(c.Query(key))
}
