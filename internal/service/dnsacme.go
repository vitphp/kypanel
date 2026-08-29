package service

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/acme"

	"kypanel/internal/config"
	"kypanel/internal/model"
)

// DNSTXTRecord 一条需要用户添加到域名解析的 TXT 记录
type DNSTXTRecord struct {
	Host  string `json:"host"`  // 完整记录名，如 _acme-challenge.baidu.com
	Zone  string `json:"zone"`  // 示例主域名，如 baidu.com（提示用户添加记录时用）
	Value string `json:"value"` // TXT 记录值
	Ok    bool   `json:"ok"`    // 解析检查结果（仅检查接口返回）
}

// DNSApplyTaskResp DNS 验证申请（第一步）返回
type DNSApplyTaskResp struct {
	TaskID  string         `json:"task_id"`
	Records []DNSTXTRecord `json:"records"`
	Domains []string       `json:"domains"`
}

// DNSPropagationCheck 解析生效检查结果
type DNSPropagationCheck struct {
	TaskID  string         `json:"task_id"`
	Records []DNSTXTRecord `json:"records"`
	OkCount int            `json:"ok_count"`
	Ready   bool           `json:"ready"`
}

// dnsTaskMeta 持久化到磁盘的 DNS 申请任务
type dnsTaskMeta struct {
	TaskID    string         `json:"task_id"`
	SiteID    uint           `json:"site_id"`
	Brand     string         `json:"brand"`
	Algorithm string         `json:"algorithm"`
	Domains   []string       `json:"domains"`
	Email     string         `json:"email"`
	OrderURL  string         `json:"order_url"`
	Records   []DNSTXTRecord `json:"records"`
	CreatedAt time.Time      `json:"created_at"`
}

func dnsTaskDir() string {
	return filepath.Join(config.Get().DataDir, "acme", "pending")
}

func dnsTaskPath(taskID string) string {
	return filepath.Join(dnsTaskDir(), taskID+".json")
}

func saveDNSTask(meta dnsTaskMeta) error {
	if err := os.MkdirAll(dnsTaskDir(), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(dnsTaskPath(meta.TaskID), data, 0o600)
}

func loadDNSTask(taskID string) (*dnsTaskMeta, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" || strings.ContainsAny(taskID, "/\\.") {
		return nil, errors.New("任务 ID 无效")
	}
	data, err := os.ReadFile(dnsTaskPath(taskID))
	if err != nil {
		return nil, errors.New("DNS 申请任务不存在或已过期，请重新发起申请")
	}
	var meta dnsTaskMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, errors.New("DNS 申请任务数据损坏，请重新发起申请")
	}
	return &meta, nil
}

func deleteDNSTask(taskID string) {
	_ = os.Remove(dnsTaskPath(taskID))
}

// cleanupExpiredDNSTasks 清理超过 24 小时的遗留任务
func cleanupExpiredDNSTasks() {
	entries, err := os.ReadDir(dnsTaskDir())
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if time.Since(info.ModTime()) > 24*time.Hour {
			_ = os.Remove(filepath.Join(dnsTaskDir(), e.Name()))
		}
	}
}

// StartDNSApply 发起 DNS 验证申请：创建 ACME 订单并返回需要添加的 TXT 记录
func StartDNSApply(req SSLApplyReq) (*DNSApplyTaskResp, error) {
	var s model.Site
	if err := model.DB.First(&s, req.ID).Error; err != nil {
		return nil, errors.New("站点不存在")
	}
	brand := strings.ToLower(req.Brand)
	if brand == "" {
		brand = "letsencrypt"
	}
	if brand != "letsencrypt" && brand != "litessl" {
		return nil, errors.New("不支持的证书品牌: " + brand)
	}
	algorithm := strings.ToLower(req.Algorithm)
	if algorithm == "" {
		algorithm = "rsa2048"
	}
	domains := cleanDomains(req.Domains)
	if len(domains) == 0 {
		return nil, errors.New("请至少选择一个域名")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, _, err := newACMEClient(brand)
	if err != nil {
		return nil, err
	}
	if err := registerACMEAccount(ctx, client, req.Email, brand); err != nil {
		return nil, err
	}

	order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(domains...))
	if err != nil {
		return nil, errors.New("创建订单失败: " + err.Error())
	}
	order, err = client.GetOrder(ctx, order.URI)
	if err != nil {
		return nil, errors.New("获取订单失败: " + err.Error())
	}

	var records []DNSTXTRecord
	for _, authzURL := range order.AuthzURLs {
		az, err := client.GetAuthorization(ctx, authzURL)
		if err != nil {
			return nil, errors.New("获取授权信息失败: " + err.Error())
		}
		if az.Status == acme.StatusValid {
			continue
		}
		var chal *acme.Challenge
		for _, c := range az.Challenges {
			if c.Type == "dns-01" {
				chal = c
				break
			}
		}
		if chal == nil {
			return nil, errors.New("域名 " + az.Identifier.Value + " 不支持 DNS 验证")
		}
		value, err := client.DNS01ChallengeRecord(chal.Token)
		if err != nil {
			return nil, errors.New("生成 TXT 记录失败: " + err.Error())
		}
		record := DNSTXTRecord{
			Host:  "_acme-challenge." + az.Identifier.Value,
			Zone:  az.Identifier.Value,
			Value: value,
		}
		dup := false
		for _, r := range records {
			if r.Host == record.Host && r.Value == record.Value {
				dup = true
				break
			}
		}
		if !dup {
			records = append(records, record)
		}
	}
	if len(records) == 0 {
		return nil, errors.New("所有域名已完成验证，可直接使用文件验证申请")
	}

	taskID := randomHex(12)
	meta := dnsTaskMeta{
		TaskID:    taskID,
		SiteID:    s.ID,
		Brand:     brand,
		Algorithm: algorithm,
		Domains:   domains,
		Email:     strings.TrimSpace(req.Email),
		OrderURL:  order.URI,
		Records:   records,
		CreatedAt: time.Now(),
	}
	if err := saveDNSTask(meta); err != nil {
		return nil, err
	}
	cleanupExpiredDNSTasks()
	return &DNSApplyTaskResp{TaskID: taskID, Records: records, Domains: domains}, nil
}

// CheckDNSPropagation 检查 TXT 记录是否已生效（通过本地 DNS 查询）
func CheckDNSPropagation(taskID string) (*DNSPropagationCheck, error) {
	meta, err := loadDNSTask(taskID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	resolver := &net.Resolver{}
	okCount := 0
	result := make([]DNSTXTRecord, 0, len(meta.Records))
	for _, r := range meta.Records {
		ok := false
		if txts, err := resolver.LookupTXT(ctx, r.Host); err == nil {
			for _, t := range txts {
				if strings.TrimSpace(t) == r.Value {
					ok = true
					break
				}
			}
		}
		if ok {
			okCount++
		}
		record := r
		record.Ok = ok
		result = append(result, record)
	}
	return &DNSPropagationCheck{
		TaskID:  meta.TaskID,
		Records: result,
		OkCount: okCount,
		Ready:   okCount == len(meta.Records),
	}, nil
}

// CompleteDNSApply 确认解析已生效后完成签发并部署证书
func CompleteDNSApply(taskID string) (string, error) {
	meta, err := loadDNSTask(taskID)
	if err != nil {
		return "", err
	}
	var s model.Site
	if err := model.DB.First(&s, meta.SiteID).Error; err != nil {
		return "", errors.New("站点不存在")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	client, _, err := newACMEClient(meta.Brand)
	if err != nil {
		return "", err
	}
	if err := registerACMEAccount(ctx, client, meta.Email, meta.Brand); err != nil {
		return "", err
	}

	siteKey, err := generateSiteKey(meta.Algorithm)
	if err != nil {
		return "", errors.New("生成站点密钥失败: " + err.Error())
	}
	csr, err := buildCSR(siteKey, meta.Domains)
	if err != nil {
		return "", errors.New("生成 CSR 失败: " + err.Error())
	}

	order, err := client.GetOrder(ctx, meta.OrderURL)
	if err != nil {
		return "", errors.New("获取订单失败: " + err.Error())
	}

	if order.Status != acme.StatusReady {
		for _, authzURL := range order.AuthzURLs {
			az, err := client.GetAuthorization(ctx, authzURL)
			if err != nil {
				return "", errors.New("获取授权信息失败: " + err.Error())
			}
			if az.Status == acme.StatusValid {
				continue
			}
			if az.Status != acme.StatusPending {
				return "", fmt.Errorf("域名 %s 验证状态异常: %s", az.Identifier.Value, az.Status)
			}
			var chal *acme.Challenge
			for _, c := range az.Challenges {
				if c.Type == "dns-01" {
					chal = c
					break
				}
			}
			if chal == nil {
				return "", errors.New("未找到 dns-01 验证方式")
			}
			if _, err := client.Accept(ctx, chal); err != nil {
				return "", errors.New("提交验证失败: " + err.Error())
			}
			if _, err := client.WaitAuthorization(ctx, az.URI); err != nil {
				return "", errors.New("域名验证失败，请确认 TXT 记录已生效（CA 校验失败）: " + err.Error())
			}
		}
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
		return "", errors.New("签发证书失败: " + err.Error())
	}
	if len(der) == 0 {
		return "", errors.New("签发证书失败: 未返回证书内容")
	}

	var fullchain strings.Builder
	for _, block := range der {
		block := &pem.Block{Type: "CERTIFICATE", Bytes: block}
		fullchain.Write(pem.EncodeToMemory(block))
	}

	privKeyDER, err := x509.MarshalPKCS8PrivateKey(siteKey)
	if err != nil {
		return "", errors.New("私钥序列化失败: " + err.Error())
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privKeyDER})

	if err := os.MkdirAll(sslDir, 0o700); err != nil {
		return "", errors.New("创建证书目录失败: " + err.Error())
	}
	certPath, keyPath := siteSSLPath(s.Name)
	if err := os.WriteFile(certPath, []byte(fullchain.String()), 0o644); err != nil {
		return "", errors.New("写入证书失败: " + err.Error())
	}
	if err := os.WriteFile(keyPath, privPEM, 0o600); err != nil {
		return "", errors.New("写入私钥失败: " + err.Error())
	}

	_ = saveACMESiteMeta(ACMESiteMeta{
		SiteID:    s.ID,
		Brand:     meta.Brand,
		Algorithm: meta.Algorithm,
		Domains:   meta.Domains,
		Email:     meta.Email,
	})

	s.SslEnabled = true
	s.SslCertPath = certPath
	s.SslKeyPath = keyPath
	s.SslForce = false
	s.ConfigOverride = ""
	if err := writeSiteConfAndReload(&s); err != nil {
		return "", err
	}
	if err := model.DB.Save(&s).Error; err != nil {
		return "", err
	}
	deleteDNSTask(taskID)
	return "通配符证书申请成功并已启用 HTTPS", nil
}

// cleanDomains 去除空项并规范小写
func cleanDomains(domains []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, d := range domains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		out = append(out, d)
	}
	return out
}

