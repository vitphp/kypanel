package service

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"strings"
	"time"

	"kypanel/internal/model"
)

// ============================ TOTP 双因素认证（RFC 6238） ============================
// 纯标准库实现 HMAC-SHA1 时间同步一次性密码，无第三方依赖。

const totpPeriod = 30 // 时间步长（秒）

// GenerateTOTPSecret 生成随机 Base32 密钥（20 字节 → 32 字符）
func GenerateTOTPSecret() string {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		// 兜底：基于时间生成，仍具随机性
		binary.BigEndian.PutUint64(b, uint64(time.Now().UnixNano()))
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
}

// totpCode 计算指定密钥在指定时间步的 6 位验证码
func totpCode(secret string, counter uint64) string {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return ""
	}
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)
	h := hmac.New(sha1.New, key)
	h.Write(buf)
	sum := h.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := (binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff) % 1000000
	return fmt.Sprintf("%06d", code)
}

// VerifyTOTP 校验验证码（允许 ±1 时间步，容忍客户端时钟偏差）
func VerifyTOTP(secret, code string) bool {
	if secret == "" || len(code) != 6 {
		return false
	}
	code = strings.TrimSpace(code)
	counter := uint64(time.Now().Unix() / totpPeriod)
	for i := -1; i <= 1; i++ {
		if totpCode(secret, counter+uint64(i)) == code {
			return true
		}
	}
	return false
}

// TOTPProvisioningURI 生成 otpauth:// URI，用于扫码绑定（Google Authenticator 等）
func TOTPProvisioningURI(secret, username string) string {
	label := "kypanel:" + username
	return "otpauth://totp/" + label + "?secret=" + secret + "&issuer=kypanel&algorithm=SHA1&digits=6&period=30"
}

// ============================ 2FA 业务接口 ============================

// TOTPStatus 返回当前管理员的 2FA 状态
func TOTPStatus(adminID uint) map[string]interface{} {
	var admin model.Admin
	if err := model.DB.First(&admin, adminID).Error; err != nil {
		return map[string]interface{}{"enabled": false}
	}
	return map[string]interface{}{"enabled": admin.TOTPSecret != ""}
}

// TOTPEnableBegin 开始启用 2FA：生成密钥并返回（暂不保存，等验证通过再保存）
func TOTPEnableBegin(adminID uint, username string) map[string]interface{} {
	secret := GenerateTOTPSecret()
	return map[string]interface{}{
		"secret": secret,
		"uri":    TOTPProvisioningURI(secret, username),
	}
}

// TOTPEnableConfirm 确认启用：校验验证码后保存密钥
func TOTPEnableConfirm(adminID uint, secret, code string) error {
	if !VerifyTOTP(secret, code) {
		return fmt.Errorf("验证码错误")
	}
	var admin model.Admin
	if err := model.DB.First(&admin, adminID).Error; err != nil {
		return err
	}
	admin.TOTPSecret = strings.ToUpper(secret)
	return model.DB.Save(&admin).Error
}

// TOTPDisable 关闭 2FA（需校验当前验证码，防止误关）
func TOTPDisable(adminID uint, code string) error {
	var admin model.Admin
	if err := model.DB.First(&admin, adminID).Error; err != nil {
		return err
	}
	if admin.TOTPSecret == "" {
		return fmt.Errorf("尚未启用 2FA")
	}
	if !VerifyTOTP(admin.TOTPSecret, code) {
		return fmt.Errorf("验证码错误")
	}
	admin.TOTPSecret = ""
	return model.DB.Save(&admin).Error
}
