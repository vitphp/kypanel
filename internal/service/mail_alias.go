package service

import (
	"errors"
	"fmt"
	"strings"

	"kypanel/internal/model"
)

// 邮箱别名：把 source@domain 收到的信转发到一个或多个目标地址（本地账号或外域地址）。

// ListMailAliases 返回某域名下的全部别名。
func ListMailAliases(domainID uint) []model.MailAlias {
	var list []model.MailAlias
	model.DB.Where("domain_id = ?", domainID).Order("id desc").Find(&list)
	return list
}

// CreateMailAlias 新增别名。
func CreateMailAlias(domainID uint, source, target, remark string) (*model.MailAlias, error) {
	dom, err := MailDomainByID(domainID)
	if err != nil {
		return nil, errors.New("域名不存在，请先添加域名")
	}
	source = strings.ToLower(strings.TrimSpace(source))
	if at := strings.IndexByte(source, '@'); at >= 0 {
		source = source[:at]
	}
	if !mailboxNameRe.MatchString(source) {
		return nil, errors.New("别名不合法（仅允许字母数字、点、下划线、连字符）")
	}
	targets, err := validateAliasTargets(target)
	if err != nil {
		return nil, err
	}
	// 与同域别名、真实账号查重
	var cnt int64
	model.DB.Model(&model.MailAlias{}).
		Where("domain_id = ? AND source = ?", domainID, source).Count(&cnt)
	if cnt > 0 {
		return nil, fmt.Errorf("别名 %s@%s 已存在", source, dom.Domain)
	}
	model.DB.Model(&model.Mailbox{}).
		Where("domain_id = ? AND name = ?", domainID, source).Count(&cnt)
	if cnt > 0 {
		return nil, fmt.Errorf("已存在同名邮箱账号 %s@%s，别名会不生效", source, dom.Domain)
	}
	rec := &model.MailAlias{
		DomainID: domainID,
		Domain:   dom.Domain,
		Source:   source,
		Target:   strings.Join(targets, ", "),
		Enabled:  true,
		Remark:   strings.TrimSpace(remark),
	}
	if err := model.DB.Create(rec).Error; err != nil {
		return nil, errors.New("创建别名失败")
	}
	return rec, nil
}

// UpdateMailAlias 更新别名（目标 / 启停 / 备注）。
func UpdateMailAlias(id uint, patch map[string]interface{}) error {
	if len(patch) == 0 {
		return nil
	}
	if t, ok := patch["target"].(string); ok {
		targets, err := validateAliasTargets(t)
		if err != nil {
			return err
		}
		patch["target"] = strings.Join(targets, ", ")
	}
	res := model.DB.Model(&model.MailAlias{}).Where("id = ?", id).Updates(patch)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("别名不存在")
	}
	return nil
}

// DeleteMailAlias 删除别名。
func DeleteMailAlias(id uint) error {
	res := model.DB.Delete(&model.MailAlias{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("别名不存在")
	}
	return nil
}

// validateAliasTargets 校验并规范化别名目标（逗号分隔，必须是合法邮箱地址）。
func validateAliasTargets(raw string) ([]string, error) {
	list := splitAddrList(raw)
	if len(list) == 0 {
		return nil, errors.New("请填写转发目标地址")
	}
	var out []string
	for _, t := range list {
		t = strings.TrimSpace(t)
		at := strings.LastIndexByte(t, '@')
		if at <= 0 || at == len(t)-1 || !strings.Contains(t[at+1:], ".") {
			return nil, fmt.Errorf("目标地址不合法：%s", t)
		}
		out = append(out, t)
	}
	return out, nil
}

// MailboxSettingsReq 账号设置（转发 / 自动回复 / 容量）
type MailboxSettingsReq struct {
	ForwardTo     *string `json:"forward_to"`
	KeepCopy      *bool   `json:"keep_copy"`
	AutoReplyOn   *bool   `json:"auto_reply_on"`
	AutoReplyText *string `json:"auto_reply_text"`
	QuotaMb       *int64  `json:"quota_mb"`
	Remark        *string `json:"remark"`
}

// UpdateMailboxSettings 更新账号设置（转发目标会做格式校验）。
func UpdateMailboxSettings(id uint, req MailboxSettingsReq) error {
	var box model.Mailbox
	if err := model.DB.First(&box, id).Error; err != nil {
		return errors.New("账号不存在")
	}
	patch := map[string]interface{}{}
	if req.ForwardTo != nil {
		v := strings.TrimSpace(*req.ForwardTo)
		if v != "" {
			if _, err := validateAliasTargets(v); err != nil {
				return err
			}
		}
		patch["forward_to"] = v
	}
	if req.KeepCopy != nil {
		patch["keep_copy"] = *req.KeepCopy
	}
	if req.AutoReplyOn != nil {
		patch["auto_reply_on"] = *req.AutoReplyOn
	}
	if req.AutoReplyText != nil {
		patch["auto_reply_text"] = strings.TrimSpace(*req.AutoReplyText)
	}
	if req.QuotaMb != nil && *req.QuotaMb > 0 {
		patch["quota_mb"] = *req.QuotaMb
	}
	if req.Remark != nil {
		patch["remark"] = strings.TrimSpace(*req.Remark)
	}
	if len(patch) == 0 {
		return nil
	}
	return model.DB.Model(&model.Mailbox{}).Where("id = ?", id).Updates(patch).Error
}
