package service

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"

	"mall-api-go/internal/config"
)

// JWTService：与 Node 版 src/utils/token.js 完全对齐。
// HS256 + 同一个 JWT_SECRET + 同样的 payload 字段，
// 所以 Node 签发的 token 能被 Go 验证、Go 签发的也能被 Node 验证（前端切换后端不掉登录）。
// payload: { type, jti, userId, role, familyId, iat, exp }
type JWTService struct {
	Secret     []byte
	AccessTTL  time.Duration
	RefreshTTL time.Duration
	DB         *sqlx.DB
	BL         *Blacklist
}

func NewJWTService(cfg config.Config, d *sqlx.DB) *JWTService {
	return &JWTService{
		Secret:     []byte(cfg.JwtSecret),
		AccessTTL:  time.Duration(cfg.AccessTTLMin) * time.Minute,
		RefreshTTL: time.Duration(cfg.RefreshTTLDays) * 24 * time.Hour,
		DB:         d,
		BL:         NewBlacklist(),
	}
}

func (j *JWTService) sign(kind, jti string, userID, role int, familyID string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"type":     kind,
		"jti":      jti,
		"userId":   userID,
		"role":     role,
		"familyId": familyID,
		"iat":      now.Unix(),
		"exp":      now.Add(ttl).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(j.Secret)
}

// IssuePair：签发「access + refresh」一对令牌，并把 refresh 的 jti 落到 RefreshToken 表。
// familyID 为空表示一次全新登录（新家族）；refresh 时沿用旧家族，logout 才能整族吊销。
func (j *JWTService) IssuePair(userID, role int, familyID string) (token, refresh, refreshJti, fid string, err error) {
	fid = familyID
	if fid == "" {
		fid = uuid.NewString()
	}
	refreshJti = uuid.NewString()
	expiresAt := time.Now().UTC().Add(j.RefreshTTL)

	// RefreshToken 表由 mall-api 的 Prisma 迁移创建，这里只写入，不改表结构。
	// expiresAt / createdAt 都按 UTC 写，和 Node 版 Prisma、PHP 版 gmdate 保持同一时间基准。
	_, err = j.DB.Exec(
		"INSERT INTO `RefreshToken` (`id`,`userId`,`familyId`,`expiresAt`,`revoked`,`createdAt`) VALUES (?,?,?,?,0,UTC_TIMESTAMP())",
		refreshJti, userID, fid, expiresAt,
	)
	if err != nil {
		return
	}
	token, err = j.sign("access", uuid.NewString(), userID, role, fid, j.AccessTTL)
	if err != nil {
		return
	}
	refresh, err = j.sign("refresh", refreshJti, userID, role, fid, j.RefreshTTL)
	return
}

func (j *JWTService) parse(raw string) (jwt.MapClaims, error) {
	claims := jwt.MapClaims{}
	tok, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != "HS256" {
			return nil, errors.New("签名算法不符")
		}
		return j.Secret, nil
	})
	if err != nil || !tok.Valid {
		return nil, errors.New("token 无效或已过期")
	}
	return claims, nil
}

// VerifyAccess：签名 / 过期 / 类型 / 黑名单四道校验。
// 兼容升级前没有 type 字段的旧令牌（按 access 处理），与 Node 版一致。
func (j *JWTService) VerifyAccess(raw string) (jwt.MapClaims, error) {
	claims, err := j.parse(raw)
	if err != nil {
		return nil, err
	}
	if typ, _ := claims["type"].(string); typ != "" && typ != "access" {
		return nil, errors.New("token 类型错误")
	}
	jti, _ := claims["jti"].(string)
	if j.BL.Has(jti) {
		return nil, errors.New("登录已失效，请重新登录")
	}
	return claims, nil
}

// VerifyRefresh：只校验签名 / 过期 / 类型，库内状态（revoke / 复用）由 handler 查表判断。
func (j *JWTService) VerifyRefresh(raw string) (jwt.MapClaims, error) {
	claims, err := j.parse(raw)
	if err != nil {
		return nil, err
	}
	if typ, _ := claims["type"].(string); typ != "refresh" {
		return nil, errors.New("token 类型错误")
	}
	return claims, nil
}

// RevokeFamily：整族吊销（logout 与「refresh 复用检测」都会调用）。
func (j *JWTService) RevokeFamily(familyID string) error {
	if familyID == "" {
		return nil
	}
	_, err := j.DB.Exec("UPDATE `RefreshToken` SET `revoked`=1 WHERE `familyId`=? AND `revoked`=0", familyID)
	return err
}

// 从 claims 里安全取整数（jwt 解码后数字是 float64）
func ClaimInt(c jwt.MapClaims, key string) int {
	switch v := c[key].(type) {
	case float64:
		return int(v)
	case int64:
		return int(v)
	case int:
		return v
	}
	return 0
}

func ClaimString(c jwt.MapClaims, key string) string {
	s, _ := c[key].(string)
	return s
}

func ClaimFloat(c jwt.MapClaims, key string) float64 {
	f, _ := c[key].(float64)
	return f
}
