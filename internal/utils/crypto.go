package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"strings"
)

// 面板数据加密（AES-GCM）。
// 用于对临时访问 token 等敏感值做"非明文存储"——即使数据库文件泄露，
// 攻击者也无法直接拿到可用的明文凭据。

var cryptoKey []byte

// InitCrypto 初始化数据加密密钥（由面板主密钥派生，须在 InitJWT 之后调用）
func InitCrypto(secret string) {
	sum := sha256.Sum256([]byte(secret))
	cryptoKey = sum[:]
}

// EncryptString AES-GCM 加密为带 enc:v1: 前缀的 base64 字符串。
// 空串返回空串（避免密文歧义）。
func EncryptString(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	if cryptoKey == nil {
		return "", errors.New("crypto not initialized")
	}
	block, err := aes.NewCipher(cryptoKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return "enc:v1:" + base64.StdEncoding.EncodeToString(ct), nil
}

// DecryptString 解密 enc:v1: 前缀的密文。
// 非 enc:v1: 前缀的值原样返回（兼容历史明文数据，如旧临时访问 token）。
func DecryptString(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	if !strings.HasPrefix(s, "enc:v1:") {
		return s, nil
	}
	if cryptoKey == nil {
		return "", errors.New("crypto not initialized")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, "enc:v1:"))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(cryptoKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// SHA256Hex 计算字符串的 SHA-256 十六进制摘要（用于可索引的凭据指纹）
func SHA256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
