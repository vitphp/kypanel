package service

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"kypanel/internal/model"
)

const litesslACMEURL = "https://acme.litessl.com/acme/v2/directory"

// CheckCertbotResult certbot 检测结果
type CheckCertbotResult struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
}

// CheckCertbot 检测证书申请能力（面板内置 ACME 客户端，无需 certbot）
func CheckCertbot() (CheckCertbotResult, error) {
	return CheckCertbotResult{Installed: true, Version: "内置 ACME 客户端（无需 certbot）"}, nil
}

// LetSEncryptReq 一键申请证书请求（旧接口兼容）
type LetSEncryptReq struct {
	ID    uint   `json:"id" binding:"required"`
	Email string `json:"email"` // 可选，接收证书过期通知
}

// SSLApplyReq 证书申请请求
type SSLApplyReq struct {
	ID        uint     `json:"id" binding:"required"`
	Brand     string   `json:"brand" binding:"required"` // letsencrypt | litessl
	Method    string   `json:"method"`                   // webroot | dns（当前仅 webroot）
	Algorithm string   `json:"algorithm"`                // rsa2048 | ecc256
	Domains   []string `json:"domains" binding:"required"`
	Email     string   `json:"email"` // 可选
}

// SSLApplyResult 申请/部署后的返回（包含证书/私钥内容供前端展示）
type SSLApplyResult struct {
	Cert string `json:"cert"`
	Key  string `json:"key"`
}

// readSiteSSLCertPair 读出当前站点的证书/私钥内容（用于前端展示）
func readSiteSSLCertPair(siteID uint) (*SSLApplyResult, error) {
	var s model.Site
	if err := model.DB.First(&s, siteID).Error; err != nil {
		return nil, errors.New("站点不存在")
	}
	if s.SslCertPath == "" || s.SslKeyPath == "" {
		return nil, errors.New("站点尚未启用 HTTPS")
	}
	cert, err := os.ReadFile(s.SslCertPath)
	if err != nil {
		return nil, errors.New("读取证书失败: " + err.Error())
	}
	key, err := os.ReadFile(s.SslKeyPath)
	if err != nil {
		return nil, errors.New("读取私钥失败: " + err.Error())
	}
	return &SSLApplyResult{Cert: string(cert), Key: string(key)}, nil
}

// RequestLetsEncrypt 一键申请 Let's Encrypt 证书（webroot 方式）并启用 HTTPS
// 成功部署后返回证书+私钥内容
func RequestLetsEncrypt(req LetSEncryptReq) (*SSLApplyResult, error) {
	var s model.Site
	if err := model.DB.First(&s, req.ID).Error; err != nil {
		return nil, errors.New("站点不存在")
	}
	domains := normalizeSSLDomains(s.Domain, s.Domains)
	if len(domains) == 0 {
		return nil, errors.New("站点未设置可申请的域名，请先在「基础设置」填写主域名")
	}
	if err := applySSLCert(s, "letsencrypt", "webroot", "rsa2048", domains, req.Email); err != nil {
		return nil, err
	}
	return readSiteSSLCertPair(s.ID)
}

// ApplySSLCert 一键申请 SSL 证书（支持 Let's Encrypt / LiteSSL）并启用 HTTPS
// 成功部署后返回证书+私钥内容
func ApplySSLCert(req SSLApplyReq) (*SSLApplyResult, error) {
	var s model.Site
	if err := model.DB.First(&s, req.ID).Error; err != nil {
		return nil, errors.New("站点不存在")
	}
	if len(req.Domains) == 0 {
		return nil, errors.New("请至少选择一个域名")
	}
	method := strings.ToLower(req.Method)
	if method == "" {
		method = "webroot"
	}
	if method != "webroot" {
		return nil, errors.New("当前仅支持文件验证（webroot）方式，DNS 验证后续开放")
	}
	if err := applySSLCert(s, strings.ToLower(req.Brand), method, strings.ToLower(req.Algorithm), req.Domains, req.Email); err != nil {
		return nil, err
	}
	return readSiteSSLCertPair(s.ID)
}

func applySSLCert(s model.Site, brand, method, algorithm string, domains []string, email string) error {
	if method == "dns" {
		return errors.New("通配符/DNS 验证暂未实现，请先使用文件验证")
	}
	for _, d := range domains {
		if strings.HasPrefix(d, "*.") {
			return errors.New("通配符域名无法通过文件验证自动申请")
		}
	}
	return issueACME(s, brand, algorithm, domains, email)
}

func normalizeSSLDomains(primary string, extra string) []string {
	var domains []string
	primary = strings.TrimSpace(primary)
	if primary != "" && !strings.HasPrefix(primary, "*.") {
		domains = append(domains, primary)
	}
	for _, d := range strings.Split(extra, ",") {
		d = strings.TrimSpace(d)
		if d != "" && !strings.HasPrefix(d, "*.") {
			domains = append(domains, d)
		}
	}
	return domains
}

// RenewLetsEncrypt 续期全部面板管理的 ACME 证书并重载 nginx（剩余 30 天内）
func RenewLetsEncrypt() (string, error) {
	return RenewLetsEncryptDays(30)
}

// RenewLetsEncryptDays 续期剩余天数 ≤ threshold 天的 ACME 证书并重载 nginx。
// threshold ≤ 0 时按 2 天处理（CLI 默认，避免频繁请求 ACME 触发速率限制）。
func RenewLetsEncryptDays(threshold int) (string, error) {
	if threshold <= 0 {
		threshold = 2
	}
	var sites []model.Site
	if err := model.DB.Where("ssl_enabled = ?", true).Find(&sites).Error; err != nil {
		return "", err
	}
	renewed := 0
	skipped := 0
	for i := range sites {
		certPath, _ := siteSSLPath(sites[i].Name)
		days, err := certExpiryDays(certPath)
		if err != nil || days > threshold {
			skipped++
			continue
		}
		if err := renewACMEForSite(&sites[i]); err != nil {
			return "", errors.New("站点 " + sites[i].Name + " 续期失败: " + err.Error())
		}
		renewed++
	}
	_ = webReload()
	if renewed == 0 {
		return fmt.Sprintf("无需续期的证书（距离到期超过 %d 天或非 ACME 证书）", threshold), nil
	}
	return fmt.Sprintf("已续期 %d 个证书，跳过 %d 个", renewed, skipped), nil
}
