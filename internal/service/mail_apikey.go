package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"kypanel/internal/model"
	"kypanel/internal/utils"
)

// 对外邮件 REST API 令牌：供第三方系统（网站、CRM 等）调用发信/读信接口。
// 只存 SHA256 哈希，明文仅在创建时返回一次；令牌绑定单一邮箱域名，天然多租户隔离。

const mailApiKeyPrefix = "mk_"

// MailApiKeyView 令牌展示视图（不含明文）
type MailApiKeyView struct {
	ID         uint      `json:"id"`
	DomainID   uint      `json:"domain_id"`
	Domain     string    `json:"domain"`
	Name       string    `json:"name"`
	KeyPrefix  string    `json:"key_prefix"`
	Enabled    bool      `json:"enabled"`
	LastUsedAt int64     `json:"last_used_at"`
	CallCount  int64     `json:"call_count"`
	CreatedAt  time.Time `json:"created_at"`
}

// ListMailApiKeys 返回某域名下的全部令牌。
func ListMailApiKeys(domainID uint) []MailApiKeyView {
	var list []model.MailApiKey
	model.DB.Where("domain_id = ?", domainID).Order("id desc").Find(&list)
	out := make([]MailApiKeyView, 0, len(list))
	for _, k := range list {
		out = append(out, MailApiKeyView{
			ID: k.ID, DomainID: k.DomainID, Domain: k.Domain, Name: k.Name,
			KeyPrefix: k.KeyPrefix, Enabled: k.Enabled, LastUsedAt: k.LastUsedAt,
			CallCount: k.CallCount, CreatedAt: k.CreatedAt,
		})
	}
	return out
}

// CreateMailApiKey 生成一个新令牌，返回明文（仅此一次）。
func CreateMailApiKey(domainID uint, name string) (plain string, view *MailApiKeyView, err error) {
	dom, err := MailDomainByID(domainID)
	if err != nil {
		return "", nil, errors.New("域名不存在，请先添加域名")
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, errors.New("生成令牌失败")
	}
	plain = mailApiKeyPrefix + hex.EncodeToString(raw)
	prefix := plain
	if len(prefix) > 11 {
		prefix = prefix[:11]
	}
	rec := &model.MailApiKey{
		DomainID:  domainID,
		Domain:    dom.Domain,
		Name:      strings.TrimSpace(name),
		KeyPrefix: prefix,
		KeyHash:   utils.SHA256Hex(plain),
		Enabled:   true,
	}
	if rec.Name == "" {
		rec.Name = "未命名"
	}
	if err := model.DB.Create(rec).Error; err != nil {
		return "", nil, errors.New("保存令牌失败")
	}
	return plain, &MailApiKeyView{
		ID: rec.ID, DomainID: rec.DomainID, Domain: rec.Domain, Name: rec.Name,
		KeyPrefix: rec.KeyPrefix, Enabled: true, CreatedAt: rec.CreatedAt,
	}, nil
}

// SetMailApiKeyEnabled 启用/停用令牌。
func SetMailApiKeyEnabled(id uint, enabled bool) error {
	res := model.DB.Model(&model.MailApiKey{}).Where("id = ?", id).Update("enabled", enabled)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("令牌不存在")
	}
	return nil
}

// DeleteMailApiKey 删除令牌。
func DeleteMailApiKey(id uint) error {
	res := model.DB.Delete(&model.MailApiKey{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("令牌不存在")
	}
	return nil
}

// AuthenticateMailApiKey 校验令牌并返回其记录（同时累加调用次数）。
func AuthenticateMailApiKey(token string) (*model.MailApiKey, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("缺少 API 令牌")
	}
	if !strings.HasPrefix(token, mailApiKeyPrefix) {
		return nil, errors.New("API 令牌格式不正确")
	}
	var rec model.MailApiKey
	if err := model.DB.Where("key_hash = ?", utils.SHA256Hex(token)).First(&rec).Error; err != nil {
		return nil, errors.New("API 令牌无效")
	}
	if !rec.Enabled {
		return nil, errors.New("API 令牌已停用")
	}
	// 域名被删/停用后令牌同步失效
	var dom model.MailDomain
	if err := model.DB.First(&dom, rec.DomainID).Error; err != nil {
		return nil, errors.New("令牌所属域名不存在")
	}
	model.DB.Model(&model.MailApiKey{}).Where("id = ?", rec.ID).Updates(map[string]interface{}{
		"last_used_at": time.Now().Unix(),
		"call_count":   rec.CallCount + 1,
	})
	return &rec, nil
}

// MailApiResolveMailbox 按 API 令牌所属域名解析发件账号（防止跨域越权）。
func MailApiResolveMailbox(key *model.MailApiKey, address string) (*model.Mailbox, error) {
	address = strings.ToLower(strings.TrimSpace(address))
	if address == "" {
		return nil, errors.New("缺少发件地址")
	}
	domain, name := splitLocalAddress(address)
	if domain != key.Domain {
		return nil, fmt.Errorf("发件地址必须属于本令牌绑定的域名 %s", key.Domain)
	}
	box, err := findMailboxByAddress(domain, name)
	if err != nil || box == nil {
		return nil, errors.New("发件账号不存在")
	}
	if !box.Enabled {
		return nil, errors.New("发件账号已停用")
	}
	return box, nil
}
