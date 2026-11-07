package httpapi

import (
	"embed"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"mall-api-go/internal/db"
	"mall-api-go/internal/service"
)

//go:embed openapi.json
var docFS embed.FS

const swaggerHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head><meta charset="utf-8"/><title>mall-api 接口文档</title>
<link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css"/></head>
<body><div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>window.onload=()=>{window.ui=SwaggerUIBundle({url:'./openapi.json',dom_id:'#swagger-ui',presets:[SwaggerUIBundle.presets.apis]})}</script>
</body></html>`

// Health：GET /api/health（公开），部署平台探活用
func (d *Deps) Health(c *gin.Context) error {
	OK(c, map[string]any{"time": time.Now().UTC().Format(time.RFC3339)})
	return nil
}

// DocsJSON / DocsHTML：与 Node 版 /api/docs 完全同路径，直接复用导出的 OpenAPI 快照
func (d *Deps) DocsJSON(c *gin.Context) error {
	b, err := docFS.ReadFile("openapi.json")
	if err != nil {
		return Err(500, "接口文档未找到")
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", b)
	return nil
}

func (d *Deps) DocsHTML(c *gin.Context) error {
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(swaggerHTML))
	return nil
}

// Register：POST /api/auth/register（公开，注册即 role=1）
func (d *Deps) Register(c *gin.Context) error {
	var req registerReq
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	u := strings.TrimSpace(req.Username)
	if len(u) < 3 || len(u) > 32 {
		return Err(400, "用户名需 3-32 个字符")
	}
	if len(req.Password) < 6 || len(req.Password) > 64 {
		return Err(400, "密码需 6-64 位")
	}
	exists, err := db.QueryOne(d.DB, "SELECT `id` FROM `User` WHERE `username`=? AND `deletedAt` IS NULL", u)
	if err != nil {
		return err
	}
	if exists != nil {
		return Err(400, "用户名已存在")
	}
	hash, err := service.HashPassword(req.Password)
	if err != nil {
		return err
	}
	nickname := req.Nickname
	if nickname == "" {
		nickname = ""
	}
	res, err := d.DB.Exec(
		"INSERT INTO `User` (`username`,`password`,`role`,`nickname`) VALUES (?,?,1,?)",
		u, hash, nickname,
	)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	return d.issueAndRespond(c, int(id), 1, map[string]any{
		"id": int(id), "username": u, "nickname": nickname, "role": 1,
	}, false)
}

// Login：POST /api/auth/login（公开）
func (d *Deps) Login(c *gin.Context) error {
	var req loginReq
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	u, err := db.QueryOne(d.DB, "SELECT * FROM `User` WHERE `username`=? AND `deletedAt` IS NULL", req.Username)
	if err != nil {
		return err
	}
	if u == nil || !service.CheckPassword(toStr(u["password"]), req.Password) {
		return Err(401, "用户名或密码错误")
	}
	return d.issueAndRespond(c, toInt(u["id"]), toInt(u["role"]), publicUser(u), false)
}

// issueAndRespond：登录/注册/微信登录共用的「签发令牌对 + 组装响应」
func (d *Deps) issueAndRespond(c *gin.Context, userID, role int, user map[string]any, wx bool) error {
	token, refresh, _, _, err := d.JWT.IssuePair(userID, role, "")
	if err != nil {
		return err
	}
	if wx {
		user = map[string]any{
			"id": user["id"], "nickname": user["nickname"],
			"role": user["role"], "avatar": user["avatar"],
		}
	}
	OK(c, map[string]any{"token": token, "refreshToken": refresh, "user": user})
	return nil
}

// loginByOpenid：按微信 openid 落地用户，首次自动建号（密码是随机哈希占位，绝不存明文）
func (d *Deps) loginByOpenid(openid, unionid string) (map[string]any, error) {
	u, err := db.QueryOne(d.DB, "SELECT * FROM `User` WHERE `openid`=? AND `deletedAt` IS NULL", openid)
	if err != nil {
		return nil, err
	}
	if u == nil {
		hash, err := service.HashPassword(openid + "_" + time.Now().String())
		if err != nil {
			return nil, err
		}
		res, err := d.DB.Exec(
			"INSERT INTO `User` (`username`,`password`,`role`,`openid`,`unionid`) VALUES (?,?,1,?,?)",
			"wx_"+openid, hash, openid, unionid,
		)
		if err != nil {
			return nil, err
		}
		id, _ := res.LastInsertId()
		u = map[string]any{"id": int(id), "nickname": nil, "role": 1, "avatar": nil}
	}
	return u, nil
}

func (d *Deps) WxLogin(c *gin.Context) error {
	var req wxCodeReq
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if req.Code == "" {
		return Err(400, "code 必填")
	}
	openid, unionid, err := service.Code2Session(d.Cfg, req.Code)
	if err != nil {
		return Err(400, err.Error())
	}
	u, err := d.loginByOpenid(openid, unionid)
	if err != nil {
		return err
	}
	return d.issueAndRespond(c, toInt(u["id"]), toInt(u["role"]), u, true)
}

func (d *Deps) WxOAuth(c *gin.Context) error {
	var req wxCodeReq
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if req.Code == "" {
		return Err(400, "code 必填")
	}
	openid, unionid, err := service.OAuthAccessToken(d.Cfg, req.Code)
	if err != nil {
		return Err(400, err.Error())
	}
	u, err := d.loginByOpenid(openid, unionid)
	if err != nil {
		return err
	}
	return d.issueAndRespond(c, toInt(u["id"]), toInt(u["role"]), u, true)
}

// Refresh：轮换 + 复用检测 + 整族吊销，逐条对应 Node 版 auth.js 的 /refresh
func (d *Deps) Refresh(c *gin.Context) error {
	var req refreshReq
	if err := bindJSON(c, &req); err != nil {
		return err
	}
	if req.RefreshToken == "" {
		return Err(400, "refreshToken 必填")
	}
	claims, err := d.JWT.VerifyRefresh(req.RefreshToken)
	if err != nil {
		return Err(401, err.Error())
	}
	jti := service.ClaimString(claims, "jti")
	rec, err := db.QueryOne(d.DB, "SELECT * FROM `RefreshToken` WHERE `id`=?", jti)
	if err != nil {
		return err
	}
	if rec == nil || toBool(rec["revoked"]) {
		return Err(401, "refresh token 已失效，请重新登录")
	}
	familyID := toStr(rec["familyId"])
	if toStr(rec["replacedBy"]) != "" {
		// 旧 refresh 被二次使用 → 疑似泄露，整族吊销强制下线
		_ = d.JWT.RevokeFamily(familyID)
		return Err(401, "检测到 refresh token 复用，已强制下线，请重新登录")
	}
	if exp := toTime(rec["expiresAt"]); !exp.IsZero() && exp.Before(time.Now()) {
		return Err(401, "refresh token 已过期，请重新登录")
	}
	u, err := db.QueryOne(d.DB, "SELECT `id`,`role` FROM `User` WHERE `id`=? AND `deletedAt` IS NULL", service.ClaimInt(claims, "userId"))
	if err != nil {
		return err
	}
	if u == nil {
		return Err(401, "用户不存在或已被删除")
	}
	token, refresh, newJti, _, err := d.JWT.IssuePair(toInt(u["id"]), toInt(u["role"]), familyID)
	if err != nil {
		return err
	}
	// 旧 refresh 用一次即作废，replacedBy 指向新 jti
	if _, err := d.DB.Exec("UPDATE `RefreshToken` SET `revoked`=1,`replacedBy`=? WHERE `id`=?", newJti, jti); err != nil {
		return err
	}
	OK(c, map[string]any{"token": token, "refreshToken": refresh})
	return nil
}

// Me：GET /api/auth/me
func (d *Deps) Me(c *gin.Context) error {
	u, err := db.QueryOne(d.DB,
		"SELECT `id`,`username`,`nickname`,`role`,`avatar`,`createdAt` FROM `User` WHERE `id`=? AND `deletedAt` IS NULL", uid(c))
	if err != nil {
		return err
	}
	if u == nil {
		return Err(401, "用户不存在或已被删除")
	}
	OK(c, u)
	return nil
}

// Logout：拉黑当前 access 的 jti（到原定过期为止）+ 整族吊销 refresh
func (d *Deps) Logout(c *gin.Context) error {
	d.JWT.BL.Add(ujti(c), uexp(c))
	if err := d.JWT.RevokeFamily(ufamily(c)); err != nil {
		return err
	}
	OK(c, map[string]any{"success": true})
	return nil
}
