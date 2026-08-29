package model

// Setting 面板键值设置
type Setting struct {
	Key   string `gorm:"primaryKey;size:64" json:"key"`
	Value string `gorm:"size:2048" json:"value"`
}

// GetSetting 读取设置
func GetSetting(key string) string {
	var s Setting
	if err := DB.First(&s, "key = ?", key).Error; err != nil {
		return ""
	}
	return s.Value
}

// SetSetting 写入设置
func SetSetting(key, value string) error {
	s := Setting{Key: key, Value: value}
	return DB.Save(&s).Error
}
