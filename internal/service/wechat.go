package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"mall-api-go/internal/config"
)

// 微信登录：与 Node 版 src/utils/wechat.js 对齐。
// 小程序/App 用 code2Session，H5 公众号网页授权用 oauth2/access_token。
// 未配置凭证时返回明确错误（不是 500），账号密码登录不受影响。

var wxHTTP = &http.Client{Timeout: 8 * time.Second}

type wxResp struct {
	OpenID      string `json:"openid"`
	UnionID     string `json:"unionid"`
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
	AccessToken string `json:"access_token"`
}

func getJSON(rawURL string) (wxResp, error) {
	var out wxResp
	resp, err := wxHTTP.Get(rawURL)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, err
	}
	return out, nil
}

// Code2Session：小程序 uni.login 拿到的 code 换 openid
func Code2Session(cfg config.Config, code string) (openid, unionid string, err error) {
	if cfg.WXAppID == "" || cfg.WXSecret == "" {
		return "", "", errors.New("未配置 WX_APPID / WX_SECRET，微信登录不可用")
	}
	q := url.Values{}
	q.Set("appid", cfg.WXAppID)
	q.Set("secret", cfg.WXSecret)
	q.Set("js_code", code)
	q.Set("grant_type", "authorization_code")
	r, err := getJSON("https://api.weixin.qq.com/sns/jscode2session?" + q.Encode())
	if err != nil {
		return "", "", err
	}
	if r.ErrCode != 0 || r.OpenID == "" {
		return "", "", errors.New("微信 code 无效或已过期: " + r.ErrMsg)
	}
	return r.OpenID, r.UnionID, nil
}

// OAuthAccessToken：公众号网页授权（snsapi_base 静默拿 openid）
func OAuthAccessToken(cfg config.Config, code string) (openid, unionid string, err error) {
	appid, secret := cfg.OAuthAppID(), cfg.OAuthSecret()
	if appid == "" || secret == "" {
		return "", "", errors.New("未配置公众号凭证（WX_OA_APPID / WX_OA_SECRET 或 WX_APPID / WX_SECRET）")
	}
	q := url.Values{}
	q.Set("appid", appid)
	q.Set("secret", secret)
	q.Set("code", code)
	q.Set("grant_type", "authorization_code")
	r, err := getJSON("https://api.weixin.qq.com/sns/oauth2/access_token?" + q.Encode())
	if err != nil {
		return "", "", err
	}
	if r.ErrCode != 0 || r.OpenID == "" {
		return "", "", errors.New("微信授权 code 无效或已过期: " + r.ErrMsg)
	}
	return r.OpenID, r.UnionID, nil
}
