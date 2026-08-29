package utils

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims 自定义 JWT 声明
type Claims struct {
	AdminID   uint   `json:"admin_id"`
	Username  string `json:"username"`
	TokenVer  int    `json:"token_ver"` // 令牌版本号：改密后 +1，旧 token 失效
	jwt.RegisteredClaims
}

var jwtSecret []byte

// InitJWT 初始化 JWT 密钥
func InitJWT(secret string) {
	jwtSecret = []byte(secret)
}

// GenerateToken 生成 Token（tokenVer 为管理员当前的令牌版本号，改密时递增使旧 token 失效）
func GenerateToken(adminID uint, username string, hours int, tokenVer int) (string, error) {
	if len(jwtSecret) == 0 {
		return "", errors.New("jwt secret not initialized")
	}
	claims := Claims{
		AdminID:  adminID,
		Username: username,
		TokenVer: tokenVer,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(hours) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "kypanel",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}

// ParseToken 解析并校验 Token
func ParseToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return jwtSecret, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}
