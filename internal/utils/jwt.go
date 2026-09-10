package utils

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Claims JWT 声明。字段名即 token 载荷的 JSON 键，改名会导致新旧 token 不兼容。
type Claims struct {
	AdminID  uint   `json:"admin_id"`
	Username string `json:"username"`
	TokenVer int    `json:"token_ver"` // 令牌版本号：改密后 +1，旧 token 失效
	Issuer   string `json:"iss,omitempty"`
	IssuedAt int64  `json:"iat,omitempty"`
	Expires  int64  `json:"exp,omitempty"`
}

var jwtSecret []byte

// jwtHeader 固定头部，与标准 JWT 一致（HS256）
var jwtHeader = []byte(`{"alg":"HS256","typ":"JWT"}`)

// InitJWT 初始化签名密钥
func InitJWT(secret string) {
	jwtSecret = []byte(secret)
}

// GenerateToken 生成 Token（tokenVer 为管理员当前的令牌版本号，改密时递增使旧 token 失效）
func GenerateToken(adminID uint, username string, hours int, tokenVer int) (string, error) {
	if len(jwtSecret) == 0 {
		return "", errors.New("jwt secret not initialized")
	}
	now := time.Now()
	payload, err := json.Marshal(Claims{
		AdminID:  adminID,
		Username: username,
		TokenVer: tokenVer,
		Issuer:   "kypanel",
		IssuedAt: now.Unix(),
		Expires:  now.Add(time.Duration(hours) * time.Hour).Unix(),
	})
	if err != nil {
		return "", err
	}
	body := b64url(jwtHeader) + "." + b64url(payload)
	return body + "." + b64url(signHMAC(body)), nil
}

// ParseToken 校验签名与有效期并解析出声明
func ParseToken(tokenStr string) (*Claims, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid token")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sig, signHMAC(parts[0]+"."+parts[1])) {
		return nil, errors.New("invalid token signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, errors.New("invalid token")
	}
	var claims Claims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return nil, errors.New("invalid token")
	}
	if claims.Expires == 0 || time.Now().Unix() > claims.Expires {
		return nil, errors.New("token expired")
	}
	return &claims, nil
}

func b64url(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func signHMAC(s string) []byte {
	m := hmac.New(sha256.New, jwtSecret)
	m.Write([]byte(s))
	return m.Sum(nil)
}
