package service

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"net/url"
	"strings"
	"sync"
	"time"

	"kypanel/internal/model"
)

// ============================ 告警通知服务 ============================
// 周期读取最新监控点，对照阈值规则触发告警，并通过配置的通知渠道推送。
// 指标：cpu / mem / disk / load（1 分钟负载）。
// 渠道：webhook / dingtalk / wecom / smtp。

const (
	alertCheckInterval = 15 * time.Second // 检测周期
	alertCooldown      = 5 * time.Minute  // 同一指标冷却期，避免刷屏
)

var (
	alertMu         sync.Mutex
	alertRunning    bool
	alertStopCh     = make(chan struct{})
	// 冷却记录：metric -> 上次告警时间
	lastAlertAt = map[string]time.Time{}
	// 连续超阈值起始时间：metric -> 首次超阈值时刻
	overStartAt = map[string]time.Time{}
	// httpClient 通知推送客户端（10s 超时）
	httpClient = &http.Client{Timeout: 10 * time.Second}
)

// StartAlert 启动后台告警检测
func StartAlert() {
	alertMu.Lock()
	defer alertMu.Unlock()
	if alertRunning {
		return
	}
	alertRunning = true
	alertStopCh = make(chan struct{})
	go alertLoop()
}

// StopAlert 停止告警检测
func StopAlert() {
	alertMu.Lock()
	defer alertMu.Unlock()
	if !alertRunning {
		return
	}
	close(alertStopCh)
	alertRunning = false
}

func alertLoop() {
	ticker := time.NewTicker(alertCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-alertStopCh:
			return
		case <-ticker.C:
			checkAlerts()
		}
	}
}

// checkAlerts 读取最新监控点，逐项判断是否触发告警
func checkAlerts() {
	cfg := model.GetAlertConfig()
	if !cfg.Enabled {
		return
	}
	pt := GetMonitorCurrent()
	if pt.Time == 0 {
		return
	}
	now := time.Now()

	type metricVal struct {
		key, name string
		value     float64
		threshold float64
		rule      model.AlertRule
	}
	metrics := []metricVal{
		{"cpu", "CPU 使用率", pt.Cpu, 0, cfg.Rules["cpu"]},
		{"mem", "内存使用率", pt.Mem, 0, cfg.Rules["mem"]},
		{"disk", "磁盘使用率", pt.Disk, 0, cfg.Rules["disk"]},
		{"load", "系统负载(1分钟)", pt.Load1, 0, cfg.Rules["load"]},
	}

	for _, m := range metrics {
		if !m.rule.Enabled || m.rule.Threshold <= 0 {
			continue
		}
		m.threshold = m.rule.Threshold
		handleMetric(now, m)
	}
}

// handleMetric 处理单个指标的告警判断（含持续时间与冷却）
func handleMetric(now time.Time, m struct {
	key, name string
	value     float64
	threshold float64
	rule      model.AlertRule
}) {
	alertMu.Lock()
	defer alertMu.Unlock()

	exceed := m.value >= m.threshold
	if exceed {
		// 记录首次超阈值时间
		if _, ok := overStartAt[m.key]; !ok {
			overStartAt[m.key] = now
		}
		// 持续时间不足，暂不告警
		if now.Sub(overStartAt[m.key]) < time.Duration(m.rule.Duration)*time.Second {
			return
		}
		// 冷却期内不重复告警
		if last, ok := lastAlertAt[m.key]; ok && now.Sub(last) < alertCooldown {
			return
		}
		lastAlertAt[m.key] = now
		fireAlert(m.key, m.name, m.value, m.threshold)
	} else {
		// 恢复正常，清除超阈值记录
		delete(overStartAt, m.key)
	}
}

// fireAlert 记录告警并推送通知
func fireAlert(metric, name string, value, threshold float64) {
	level := "warning"
	if value >= threshold*1.1 {
		level = "critical"
	}
	msg := fmt.Sprintf("[%s] %s %.1f%% 超过阈值 %.1f%%", level, name, value, threshold)
	if metric == "load" {
		msg = fmt.Sprintf("[%s] %s %.2f 超过阈值 %.2f", level, name, value, threshold)
	}

	log := model.AlertLog{
		Metric:    metric,
		Value:     value,
		Threshold: threshold,
		Level:     level,
		Message:   msg,
		Notified:  false,
	}
	if err := model.DB.Create(&log).Error; err != nil {
		return
	}

	// 推送通知
	cfg := model.GetAlertConfig()
	if sendAlertNotifications(cfg, msg, level) {
		model.DB.Model(&log).Update("notified", true)
	}
}

// ============================ 通知渠道 ============================

// sendAlertNotifications 遍历启用渠道发送通知，返回是否有任一渠道成功
func sendAlertNotifications(cfg model.AlertConfig, msg, level string) bool {
	anyOK := false
	for _, ch := range cfg.Channels {
		if !ch.Enabled {
			continue
		}
		var err error
		switch ch.Type {
		case "webhook":
			err = sendWebhook(ch.URL, msg, level)
		case "dingtalk":
			err = sendDingTalk(ch, msg)
		case "wecom":
			err = sendWeCom(ch, msg)
		case "smtp":
			err = sendSMTP(ch, msg)
		}
		if err == nil {
			anyOK = true
		}
	}
	return anyOK
}

// sendWebhook 通用 Webhook（POST JSON）
func sendWebhook(webhookURL, msg, level string) error {
	if webhookURL == "" {
		return fmt.Errorf("webhook url 为空")
	}
	body, _ := json.Marshal(map[string]string{
		"msgtype": "text",
		"content": msg,
		"level":   level,
	})
	req, err := http.NewRequest("POST", webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook 返回 %d", resp.StatusCode)
	}
	return nil
}

// sendDingTalk 钉钉机器人（支持加签）
func sendDingTalk(ch model.AlertChannel, msg string) error {
	if ch.URL == "" {
		return fmt.Errorf("钉钉 URL 为空")
	}
	u := ch.URL
	if ch.Secret != "" {
		timestamp := time.Now().UnixMilli()
		sign := dingTalkSign(ch.Secret, timestamp)
		sep := "&"
		if !strings.Contains(u, "?") {
			sep = "?"
		}
		u = u + sep + fmt.Sprintf("timestamp=%d&sign=%s", timestamp, sign)
	}
	body, _ := json.Marshal(map[string]interface{}{
		"msgtype": "text",
		"text":    map[string]string{"content": msg},
	})
	resp, err := http.Post(u, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// dingTalkSign 钉钉加签算法
func dingTalkSign(secret string, timestamp int64) string {
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(stringToSign))
	return url.QueryEscape(base64.StdEncoding.EncodeToString(h.Sum(nil)))
}

// sendWeCom 企业微信机器人
func sendWeCom(ch model.AlertChannel, msg string) error {
	if ch.URL == "" {
		return fmt.Errorf("企业微信 URL 为空")
	}
	body, _ := json.Marshal(map[string]interface{}{
		"msgtype": "text",
		"text":    map[string]string{"content": msg},
	})
	resp, err := http.Post(ch.URL, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// sendSMTP 邮件通知。URL 存 JSON：{"host":"","port":465,"user":"","pass":"","to":"a@x.com,b@y.com"}
func sendSMTP(ch model.AlertChannel, msg string) error {
	if ch.URL == "" {
		return fmt.Errorf("SMTP 配置为空")
	}
	var sc struct {
		Host string `json:"host"`
		Port int    `json:"port"`
		User string `json:"user"`
		Pass string `json:"pass"`
		To   string `json:"to"`
	}
	if err := json.Unmarshal([]byte(ch.URL), &sc); err != nil {
		return err
	}
	if sc.Host == "" || sc.To == "" {
		return fmt.Errorf("SMTP host/to 未配置")
	}
	if sc.Port == 0 {
		sc.Port = 465
	}
	subject := "kypanel 告警通知"
	body := "To: " + sc.To + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"\r\n" + msg

	addr := fmt.Sprintf("%s:%d", sc.Host, sc.Port)
	auth := smtp.PlainAuth("", sc.User, sc.Pass, sc.Host)
	// 465 端口用 TLS；25/587 用普通（此处统一 TLS，覆盖主流 SMTP SSL）
	return smtp.SendMail(addr, auth, sc.User, strings.Split(sc.To, ","), []byte(body))
}

// ============================ 配置与查询 API ============================

// GetAlertSettings 返回告警配置 + 最近告警记录
func GetAlertSettings() map[string]interface{} {
	cfg := model.GetAlertConfig()
	var logs []model.AlertLog
	model.DB.Order("id desc").Limit(100).Find(&logs)
	return map[string]interface{}{
		"config": cfg,
		"logs":   logs,
	}
}

// UpdateAlertSettings 更新告警配置
func UpdateAlertSettings(cfg model.AlertConfig) error {
	if cfg.Rules == nil {
		cfg.Rules = map[string]model.AlertRule{}
	}
	return model.SaveAlertConfig(cfg)
}

// TestAlertChannel 测试单个通知渠道
func TestAlertChannel(ch model.AlertChannel) error {
	switch ch.Type {
	case "webhook":
		return sendWebhook(ch.URL, "kypanel 测试告警：这是一条测试消息", "info")
	case "dingtalk":
		return sendDingTalk(ch, "kypanel 测试告警：这是一条测试消息")
	case "wecom":
		return sendWeCom(ch, "kypanel 测试告警：这是一条测试消息")
	case "smtp":
		return sendSMTP(ch, "kypanel 测试告警：这是一条测试消息")
	default:
		return fmt.Errorf("不支持的渠道类型: %s", ch.Type)
	}
}

// ClearAlertLogs 清空告警历史
func ClearAlertLogs() error {
	return model.DB.Where("1 = 1").Delete(&model.AlertLog{}).Error
}
