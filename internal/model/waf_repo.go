package model

import "time"

// ============================================================================
// WAF 数据访问层（repository）：收敛 WAFSetting / WAFRule / WAFIpRule /
// WAFCcConfig / WAFAttackLog 的数据库读写，供 service 层复用。
// ============================================================================

// --- WAFSetting ---

// GetWAFSetting 按 key 查询 WAF 设置，返回是否找到
func GetWAFSetting(key string) (*WAFSetting, bool) {
	var s WAFSetting
	if err := DB.First(&s, "key = ?", key).Error; err != nil {
		return nil, false
	}
	return &s, true
}

// SaveWAFSetting 保存 WAF 设置（存在则更新，否则创建）
func SaveWAFSetting(s *WAFSetting) error {
	return DB.Save(s).Error
}

// --- WAFRule ---

// CountBuiltinWAFRules 统计内置规则数量
func CountBuiltinWAFRules() (int64, error) {
	var c int64
	err := DB.Model(&WAFRule{}).Where("builtin = ?", true).Count(&c).Error
	return c, err
}

// CreateWAFRule 创建规则（全字段）
func CreateWAFRule(rule *WAFRule) error {
	return DB.Create(rule).Error
}

// CreateWAFRuleWithFields 创建规则（仅写入指定字段，用于自定义规则避免零值被默认值覆盖）
func CreateWAFRuleWithFields(rule *WAFRule) error {
	return DB.Select("Category", "CategoryCN", "Name", "Pattern", "MatchField", "Action", "Enabled", "Builtin", "Priority", "Remark").Create(rule).Error
}

// ListWAFRulesOrdered 按 id 排序查询规则
func ListWAFRulesOrdered(order string) ([]WAFRule, error) {
	var rules []WAFRule
	err := DB.Order(order).Find(&rules).Error
	return rules, err
}

// ListEnabledWAFRules 查询启用的规则（按优先级降序）
func ListEnabledWAFRules() ([]WAFRule, error) {
	var rules []WAFRule
	err := DB.Where("enabled = ?", true).Order("priority desc, id asc").Find(&rules).Error
	return rules, err
}

// GetWAFRule 按 id 查询规则
func GetWAFRule(id uint) (*WAFRule, error) {
	var rule WAFRule
	if err := DB.First(&rule, id).Error; err != nil {
		return nil, err
	}
	return &rule, nil
}

// SaveWAFRule 保存规则
func SaveWAFRule(rule *WAFRule) error {
	return DB.Save(rule).Error
}

// DeleteWAFRule 删除规则
func DeleteWAFRule(rule *WAFRule) error {
	return DB.Delete(rule).Error
}

// DeleteBuiltinWAFRules 删除所有内置规则
func DeleteBuiltinWAFRules() error {
	return DB.Where("builtin = ?", true).Delete(&WAFRule{}).Error
}

// --- WAFIpRule ---

// ListAllWAFIpRules 按 id 降序查询所有黑白名单规则
func ListAllWAFIpRules() ([]WAFIpRule, error) {
	var rules []WAFIpRule
	err := DB.Order("id desc").Find(&rules).Error
	return rules, err
}

// CreateWAFIpRule 创建黑白名单规则
func CreateWAFIpRule(rule *WAFIpRule) error {
	return DB.Create(rule).Error
}

// DeleteWAFIpRule 按 id 删除黑白名单规则
func DeleteWAFIpRule(id uint) error {
	return DB.Delete(&WAFIpRule{}, id).Error
}

// ListWAFIpRules 按类型+动作查询黑白名单
func ListWAFIpRules(ruleType, action string) ([]WAFIpRule, error) {
	var rules []WAFIpRule
	err := DB.Where("type = ? AND action = ?", ruleType, action).Find(&rules).Error
	return rules, err
}

// ListExpiredWAFIpRules 查询已过期的临时封禁规则
func ListExpiredWAFIpRules(ruleType, action string, before time.Time) ([]WAFIpRule, error) {
	var rules []WAFIpRule
	err := DB.Where("type = ? AND action = ? AND expire_at IS NOT NULL AND expire_at < ?", ruleType, action, before).Find(&rules).Error
	return rules, err
}

// DeleteWAFIpRulesByIDs 批量删除黑白名单规则
func DeleteWAFIpRulesByIDs(ids []uint) error {
	return DB.Where("id IN ?", ids).Delete(&WAFIpRule{}).Error
}

// --- WAFCcConfig ---

// GetWAFCcConfig 获取 CC 配置（单行表）
func GetWAFCcConfig() (*WAFCcConfig, error) {
	var cfg WAFCcConfig
	if err := DB.First(&cfg).Error; err != nil {
		return nil, err
	}
	return &cfg, nil
}

// SaveWAFCcConfig 保存 CC 配置
func SaveWAFCcConfig(cfg *WAFCcConfig) error {
	return DB.Save(cfg).Error
}

// --- WAFAttackLog ---

// CreateWAFAttackLog 记录攻击日志
func CreateWAFAttackLog(log *WAFAttackLog) error {
	return DB.Create(log).Error
}

// CountWAFAttackLogs 统计攻击日志数量（可选条件）
func CountWAFAttackLogs(query interface{}, args ...interface{}) (int64, error) {
	var c int64
	err := DB.Model(&WAFAttackLog{}).Where(query, args...).Count(&c).Error
	return c, err
}

// FirstWAFAttackLogOrdered 查询第一条攻击日志
func FirstWAFAttackLogOrdered(order string) (*WAFAttackLog, error) {
	var first WAFAttackLog
	if err := DB.Order(order).First(&first).Error; err != nil {
		return nil, err
	}
	return &first, nil
}

// DeleteWAFAttackLogsBefore 删除指定 id 之前的攻击日志
func DeleteWAFAttackLogsBefore(id uint) error {
	return DB.Where("id <= ?", id).Delete(&WAFAttackLog{}).Error
}

// ClearWAFAttackLogs 清空所有攻击日志
func ClearWAFAttackLogs() error {
	return DB.Where("1 = 1").Delete(&WAFAttackLog{}).Error
}
