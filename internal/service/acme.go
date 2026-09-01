package service

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/acme"

	"kypanel/internal/config"
	"kypanel/internal/model"
)

const (
	letsencryptACMEURL = "https://acme-v02.api.letsencrypt.org/directory"
)

// ACMESiteMeta 记录站点证书的 ACME 申请信息（用于续期）
type ACMESiteMeta struct {
	SiteID    uint     `json:"site_id"`
	Brand     string   `json:"brand"`
	Algorithm string   `json:"algorithm"`
	Domains   []string `json:"domains"`
	Email     string   `json:"email"`
}

func acmeAccountKeyPath(brand string) string {
	return filepath.Join(config.Get().DataDir, "acme", "accounts", brand+".key")
}

func acmeSiteMetaPath(siteID uint) string {
	return filepath.Join(config.Get().DataDir, "acme", "sites", fmt.Sprintf("%d.json", siteID))
}

func saveACMESiteMeta(meta ACMESiteMeta) error {
	dir := filepath.Dir(acmeSiteMetaPath(meta.SiteID))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(acmeSiteMetaPath(meta.SiteID), data, 0o600)
}

func loadACMESiteMeta(siteID uint) (ACMESiteMeta, bool) {
	data, err := os.ReadFile(acmeSiteMetaPath(siteID))
	if err != nil {
		return ACMESiteMeta{}, false
	}
	var meta ACMESiteMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return ACMESiteMeta{}, false
	}
	return meta, true
}

// loadOrCreateAccountKey 加载或生成 ACME 账户私钥
func loadOrCreateAccountKey(brand string) (*rsa.PrivateKey, error) {
	path := acmeAccountKeyPath(brand)
	if data, err := os.ReadFile(path); err == nil {
		block, _ := pem.Decode(data)
		if block != nil {
			if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
				if rk, ok := k.(*rsa.PrivateKey); ok {
					return rk, nil
				}
			}
		}
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

// decodeEABKey 解析 LiteSSL 提供的 EAB HMAC Key（base64url / base64 / 明文均可）
func decodeEABKey(s string) []byte {
	s = strings.TrimSpace(s)
	if raw, err := base64.RawURLEncoding.DecodeString(s); err == nil && len(raw) > 0 {
		return raw
	}
	if raw, err := base64.URLEncoding.DecodeString(s); err == nil && len(raw) > 0 {
		return raw
	}
	if raw, err := base64.StdEncoding.DecodeString(s); err == nil && len(raw) > 0 {
		return raw
	}
	return []byte(s)
}

// newACMEClient 创建 ACME 客户端（Let's Encrypt / LiteSSL）
func newACMEClient(brand string) (*acme.Client, *rsa.PrivateKey, error) {
	dir := letsencryptACMEURL
	if brand == "litessl" {
		dir = litesslACMEURL
	}
	key, err := loadOrCreateAccountKey(brand)
	if err != nil {
		return nil, nil, errors.New("生成 ACME 账户密钥失败: " + err.Error())
	}
	return &acme.Client{DirectoryURL: dir, Key: key}, key, nil
}

// registerACMEAccount 注册（或复用）ACME 账户
func registerACMEAccount(ctx context.Context, client *acme.Client, email string, brand string) error {
	eab := GetLiteSSLSetting()
	var binding *acme.ExternalAccountBinding
	if brand == "litessl" {
		if eab.EabKid == "" || eab.EabHmac == "" {
			return errors.New("使用 LiteSSL 前请先在「面板设置 → 证书」中配置 EAB KID 和 EAB HMAC Key")
		}
		binding = &acme.ExternalAccountBinding{
			KID: eab.EabKid,
			Key: decodeEABKey(eab.EabHmac),
		}
	}
	acct := &acme.Account{ExternalAccountBinding: binding}
	if email = strings.TrimSpace(email); email != "" {
		acct.Contact = []string{"mailto:" + email}
	}
	_, err := client.Register(ctx, acct, acme.AcceptTOS)
	if err != nil {
		// 账户已存在时 Register 会报 ErrAccountAlreadyExists，忽略即可
		if !errors.Is(err, acme.ErrAccountAlreadyExists) {
			return errors.New("注册 ACME 账户失败: " + err.Error())
		}
	}
	return nil
}

// generateSiteKey 生成站点证书私钥
func generateSiteKey(algorithm string) (crypto.Signer, error) {
	if algorithm == "ecc256" {
		return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	}
	return rsa.GenerateKey(rand.Reader, 2048)
}

// buildCSR 生成证书签名请求
func buildCSR(key crypto.Signer, domains []string) ([]byte, error) {
	var sigAlgo x509.SignatureAlgorithm
	switch key.(type) {
	case *ecdsa.PrivateKey:
		sigAlgo = x509.ECDSAWithSHA256
	case *rsa.PrivateKey:
		sigAlgo = x509.SHA256WithRSA
	default:
		sigAlgo = x509.UnknownSignatureAlgorithm
	}
	tpl := &x509.CertificateRequest{
		Subject:            pkix.Name{CommonName: domains[0]},
		DNSNames:           domains,
		SignatureAlgorithm: sigAlgo,
	}
	return x509.CreateCertificateRequest(rand.Reader, tpl, key)
}

// solveHTTP01Challenges 通过文件验证完成授权（HTTP-01）
// 关键设计：
//  1. 多域名（如 qb.n.05v.cn + ca.n.05v.cn）时 LE 会并行验证所有域名，
//     必须先写好全部验证文件再逐个提交，避免后续域名文件尚未写入就被 LE 访问而 404。
//  2. LE 对每个域名有 primary + secondary 多路径验证，secondary 可能延迟数分钟
//     才从另一节点发起。因此验证完成（含失败）后不删除验证文件，改由下一次
//     申请前统一清理，防止延迟节点访问时文件已删而 404（表现为“第一次失败第二次成功”）。
func solveHTTP01Challenges(ctx context.Context, client *acme.Client, order *acme.Order, webRoot string) error {
	chalDir := filepath.Join(webRoot, ".well-known", "acme-challenge")
	if err := os.MkdirAll(chalDir, 0o755); err != nil {
		return errors.New("创建验证目录失败: " + err.Error())
	}
	// 清理上次申请遗留的旧验证文件，避免目录持续累积
	cleanupChallengeDir(chalDir)

	type pending struct {
		az   *acme.Authorization
		chal *acme.Challenge
	}
	var pendings []pending
	for _, authzURL := range order.AuthzURLs {
		az, err := client.GetAuthorization(ctx, authzURL)
		if err != nil {
			return errors.New("获取授权信息失败: " + err.Error())
		}
		if az.Status == acme.StatusValid {
			continue
		}
		var chal *acme.Challenge
		for _, c := range az.Challenges {
			if c.Type == "http-01" {
				chal = c
				break
			}
		}
		if chal == nil {
			return errors.New("未找到 http-01 验证方式（请确认 80 端口可访问且域名已解析到本服务器）")
		}
		keyAuth, err := client.HTTP01ChallengeResponse(chal.Token)
		if err != nil {
			return err
		}
		tokenPath := filepath.Join(chalDir, chal.Token)
		if err := os.WriteFile(tokenPath, []byte(keyAuth), 0o644); err != nil {
			return errors.New("写入验证文件失败: " + err.Error())
		}
		pendings = append(pendings, pending{az: az, chal: chal})
	}
	// 所有域名的验证文件均已就绪后再逐个提交
	for _, p := range pendings {
		if _, err := client.Accept(ctx, p.chal); err != nil {
			return errors.New("提交验证失败: " + err.Error())
		}
	}
	// 等待全部域名验证完成。验证文件不在此处删除，理由见函数注释。
	for _, p := range pendings {
		if _, err := client.WaitAuthorization(ctx, p.az.URI); err != nil {
			return errors.New("域名验证失败: " + err.Error())
		}
	}
	return nil
}

// cleanupChallengeDir 清理 acme-challenge 目录下遗留的旧验证文件
func cleanupChallengeDir(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// issueACME 使用内置 ACME 客户端申请证书并部署到站点（无需 certbot / acme.sh）
func issueACME(s model.Site, brand, algorithm string, domains []string, email string) error {
	if brand == "" {
		brand = "letsencrypt"
	}
	if brand != "letsencrypt" && brand != "litessl" {
		return errors.New("不支持的证书品牌: " + brand)
	}
	if algorithm == "" {
		algorithm = "rsa2048"
	}
	// 计算 Nginx 实际服务 .well-known 的 root（验证文件必须写到这里才能被访问到）
	// root 类型（static/php）：server root 含运行目录 RuntimeDir；
	// 反代类型（node/python/go/proxy）：location 块显式指向站点根目录 s.Root
	webRoot := s.Root
	if model.IsRootType(s.Type) {
		if rd := strings.TrimSpace(s.RuntimeDir); rd != "" {
			webRoot = filepath.Join(webRoot, strings.Trim(rd, "/"))
		}
	}
	if _, err := os.Stat(s.Root); err != nil {
		// 根目录不存在时自动创建（反代站点可能没有实际文件目录）
		if err := os.MkdirAll(s.Root, 0o755); err != nil {
			return errors.New("站点根目录不存在且无法创建: " + s.Root + "，" + err.Error())
		}
	}
	if webRoot != s.Root {
		if _, err := os.Stat(webRoot); err != nil {
			return errors.New("站点运行目录不存在: " + webRoot + "，请先创建目录")
		}
	}

	// 重新生成站点 Nginx 配置，确保 .well-known 验证目录与运行目录一致（已存在站点同步新逻辑）
	// 注意：配置了自定义 Nginx 配置（ConfigOverride）的站点由用户自行管理 .well-known，跳过自动重写
	if strings.TrimSpace(s.ConfigOverride) == "" {
		if err := writeSiteConfAndReload(&s); err != nil {
			return errors.New("更新站点配置并重载失败: " + err.Error())
		}
	}

	// 等待时间放宽到 10 分钟：LE 对域名有 primary + secondary 多路径验证，
	// secondary 可能延迟数分钟；验证失败后的自动重试（见下）也会占用部分时间。
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
	defer cancel()

	client, _, err := newACMEClient(brand)
	if err != nil {
		return err
	}
	if err := registerACMEAccount(ctx, client, email, brand); err != nil {
		return err
	}

	siteKey, err := generateSiteKey(algorithm)
	if err != nil {
		return errors.New("生成站点密钥失败: " + err.Error())
	}
	csr, err := buildCSR(siteKey, domains)
	if err != nil {
		return errors.New("生成 CSR 失败: " + err.Error())
	}

	// 域名验证（HTTP-01）自动重试：
	// LE 对每个域名有 primary + secondary 多路径验证，secondary 节点可能延迟数分钟
	// 才从另一节点发起请求，且该节点可能未及时拉取到验证文件，导致首次验证失败、
	// 稍后重试成功（面板日志里常见的"第一次失败第二次成功"）。因此验证失败后
	// 等待片刻再重新创建订单（旧订单验证失败后已不可复用）并重试一次，能显著
	// 提高首次申请成功率，用户无需反复手动点「申请」。
	const maxVerifyAttempts = 2
	order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(domains...))
	if err != nil {
		return errors.New("创建订单失败: " + err.Error())
	}
	order, err = client.GetOrder(ctx, order.URI)
	if err != nil {
		return errors.New("获取订单失败: " + err.Error())
	}
	verified := false
	for attempt := 1; attempt <= maxVerifyAttempts && !verified; attempt++ {
		if order.Status == acme.StatusReady {
			verified = true
			break
		}
		if err := solveHTTP01Challenges(ctx, client, order, webRoot); err != nil {
			if attempt < maxVerifyAttempts {
				// 等待 LE secondary 验证节点就绪后重试
				select {
				case <-time.After(10 * time.Second):
				case <-ctx.Done():
					return ctx.Err()
				}
				order, err = client.AuthorizeOrder(ctx, acme.DomainIDs(domains...))
				if err != nil {
					return errors.New("创建订单失败: " + err.Error())
				}
				order, err = client.GetOrder(ctx, order.URI)
				if err != nil {
					return errors.New("获取订单失败: " + err.Error())
				}
				continue
			}
			return err
		}
		verified = true
	}

	// CSR 必须提交到 finalize 端点（order.FinalizeURL），而非订单端点（order.URI）。
	// 订单端点仅接受 GET/POST-as-GET，向其 POST 带 CSR 的请求会被服务器按 POST-as-GET 校验
	// 并返回 "POST-as-GET requests must have an empty payload" 400 错误。
	finalizeURL := order.FinalizeURL
	if finalizeURL == "" {
		finalizeURL = order.URI // 兜底，兼容未返回 finalize 字段的服务器
	}
	der, _, err := client.CreateOrderCert(ctx, finalizeURL, csr, true)
	if err != nil {
		return errors.New("签发证书失败: " + err.Error())
	}
	if len(der) == 0 {
		return errors.New("签发证书失败: 未返回证书内容")
	}

	var fullchain strings.Builder
	for i, block := range der {
		block := &pem.Block{Type: "CERTIFICATE", Bytes: block}
		fullchain.Write(pem.EncodeToMemory(block))
		if i == 0 {
			// 提前校验证书可解析
			if _, err := x509.ParseCertificate(block.Bytes); err != nil {
				return errors.New("证书解析失败: " + err.Error())
			}
		}
	}

	privKeyDER, err := x509.MarshalPKCS8PrivateKey(siteKey)
	if err != nil {
		return errors.New("私钥序列化失败: " + err.Error())
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privKeyDER})

	if err := os.MkdirAll(sslDir, 0o700); err != nil {
		return errors.New("创建证书目录失败: " + err.Error())
	}
	certPath, keyPath := siteSSLPath(s.Name)
	if err := os.WriteFile(certPath, []byte(fullchain.String()), 0o644); err != nil {
		return errors.New("写入证书失败: " + err.Error())
	}
	if err := os.WriteFile(keyPath, privPEM, 0o600); err != nil {
		return errors.New("写入私钥失败: " + err.Error())
	}

	// 记录 ACME 元信息用于续期
	_ = saveACMESiteMeta(ACMESiteMeta{
		SiteID:    s.ID,
		Brand:     brand,
		Algorithm: algorithm,
		Domains:   domains,
		Email:     email,
	})

	// 解析证书信息写入 SSL 证书记录表
	firstCert, _ := x509.ParseCertificate(der[0])
	if firstCert != nil {
		days := int(time.Until(firstCert.NotAfter).Hours() / 24)
		record := model.SSLCertRecord{
			Name:      s.Name,
			Brand:     brand,
			Domains:   strings.Join(domains, ","),
			CertPath:  certPath,
			KeyPath:   keyPath,
			Algorithm: algorithm,
			Email:     email,
			NotBefore: firstCert.NotBefore,
			NotAfter:  firstCert.NotAfter,
			Days:      days,
		}
		// upsert：同名证书更新，不存在则创建
		var existing model.SSLCertRecord
		if err := model.DB.Where("name = ?", s.Name).First(&existing).Error; err == nil {
			record.ID = existing.ID
			_ = model.DB.Save(&record).Error
		} else {
			_ = model.DB.Create(&record).Error
		}
	}

	s.SslEnabled = true
	s.SslCertPath = certPath
	s.SslKeyPath = keyPath
	s.SslForce = false
	s.ConfigOverride = ""
	if err := writeSiteConfAndReload(&s); err != nil {
		return err
	}
	return model.DB.Save(&s).Error
}

// renewACMEForSite 续期单个站点的 ACME 证书
// 注意：含通配符域名的证书需用户重新在面板中手动申请（DNS 验证需要重新添加 TXT 记录）
func renewACMEForSite(s *model.Site) error {
	meta, ok := loadACMESiteMeta(s.ID)
	if !ok || len(meta.Domains) == 0 {
		return nil
	}
	certPath, _ := siteSSLPath(s.Name)
	if days, err := certExpiryDays(certPath); err == nil && days > 30 {
		return nil
	}
	for _, d := range meta.Domains {
		if strings.HasPrefix(d, "*.") {
			return nil // 通配符证书跳过自动续期，由用户手动重新申请
		}
	}
	return issueACME(*s, meta.Brand, meta.Algorithm, meta.Domains, meta.Email)
}
