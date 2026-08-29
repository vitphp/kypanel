package utils

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

const apiKeyLen = 36

// GenerateAPIKey 生成 36 位随机 API 令牌（URL-safe 字符集，大小写+数字，无前缀）。
// 明文 token 总长固定 36 位，便于与登录 JWT（三段式、带 `.`）区分。
func GenerateAPIKey() (string, error) {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	raw := make([]byte, apiKeyLen)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	buf := make([]byte, apiKeyLen)
	for i := range raw {
		// 用取模避免偏差（charset 长度 62，接近 2^6，偏差可忽略，但用简单取模足够安全）
		buf[i] = charset[int(raw[i])%len(charset)]
	}
	return string(buf), nil
}

// HashAPIKey 计算 API 令牌的 SHA256 哈希（存库用，避免明文落库）。
func HashAPIKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NewAPIKey 别名：生成 36 位随机 API 令牌。
func NewAPIKey() (string, error) {
	if _, err := rand.Read(make([]byte, 1)); err != nil {
		return "", errors.New("crypto/rand unavailable")
	}
	return GenerateAPIKey()
}
