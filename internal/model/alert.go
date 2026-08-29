package model

import (
	"encoding/json"
	"time"
)

// AlertLog 一条告警历史记录
type AlertLog struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	Metric    string    `gorm:"size:32;index" json:"metric"` // cpu / mem / disk / load
	Value     float64   `json:"value"`                       // 触发时的指标值
	Threshold float64   `json:"threshold"`                   // 阈值
	Level     string    `gorm:"size:16" json:"level"`        // warning / critical
	Message   string    `gorm:"size:512" json:"message"`     // 告警内容
	Notified  bool      `gorm:"default:false" json:"notified"`
	CreatedAt time.Time `json:"created_at"`
}

// AlertRule 单条告警规则
type AlertRule struct {
	Enabled   bool    `json:"enabled"`   // 是否启用该指标告警
	Threshold float64 `json:"threshold"` // 阈值（百分比或负载值）
	Duration  int     `json:"duration"`  // 持续时间（秒），连续超过阈值才告警
}

// AlertChannel 通知渠道
type AlertChannel struct {
	Type    string `json:"type"`    // webhook / dingtalk / wecom / smtp
	Name    string `json:"name"`    // 渠道名称
	Enabled bool   `json:"enabled"` // 是否启用
	// webhook/dingtalk/wecom 用 URL；smtp 用 JSON 字符串（host/port/user/pass/to）
	URL    string `json:"url"`
	Secret string `json:"secret,omitempty"` // 钉钉/企微签名密钥
}

// AlertConfig 告警配置（整体）
type AlertConfig struct {
	Enabled  bool                   `json:"enabled"` // 告警总开关
	Rules    map[string]AlertRule   `json:"rules"`   // metric -> rule
	Channels []AlertChannel         `json:"channels"`
}

// defaultAlertConfig 默认配置
func defaultAlertConfig() AlertConfig {
	return AlertConfig{
		Enabled: false,
		Rules: map[string]AlertRule{
			"cpu":  {Enabled: true, Threshold: 90, Duration: 60},
			"mem":  {Enabled: true, Threshold: 90, Duration: 60},
			"disk": {Enabled: true, Threshold: 90, Duration: 60},
			"load": {Enabled: true, Threshold: 8, Duration: 60},
		},
		Channels: []AlertChannel{},
	}
}

// GetAlertConfig 读取告警配置（从 Setting 的 JSON 反序列化）
func GetAlertConfig() AlertConfig {
	cfg := defaultAlertConfig()
	if raw := GetSetting("alert_config"); raw != "" {
		var saved AlertConfig
		if err := json.Unmarshal([]byte(raw), &saved); err == nil {
			// 合并：未保存的字段用默认值
			if saved.Rules != nil {
				cfg.Rules = saved.Rules
			}
			if saved.Channels != nil {
				cfg.Channels = saved.Channels
			}
			cfg.Enabled = saved.Enabled
		}
	}
	return cfg
}

// SaveAlertConfig 保存告警配置
func SaveAlertConfig(cfg AlertConfig) error {
	b, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return SetSetting("alert_config", string(b))
}
