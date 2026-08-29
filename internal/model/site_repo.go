package model

// ============================================================================
// Site 数据访问层（repository）：收敛 Site / SiteRedirect 的数据库读写。
// ============================================================================

// GetSite 按 ID 查询站点，返回是否找到
func GetSite(id uint) (*Site, bool) {
	var s Site
	if err := DB.First(&s, id).Error; err != nil {
		return nil, false
	}
	return &s, true
}

// ListSites 查询全部站点（按 id 降序，新建的在前）
func ListSites() ([]Site, error) {
	var list []Site
	err := DB.Order("id desc").Find(&list).Error
	return list, err
}

// ListSitesByIDs 按 ID 列表查询站点
func ListSitesByIDs(ids []uint) ([]Site, error) {
	var list []Site
	err := DB.Where("id IN ?", ids).Find(&list).Error
	return list, err
}

// GetSiteByName 按站点名查询
func GetSiteByName(name string) (*Site, bool) {
	var s Site
	if err := DB.First(&s, "name = ?", name).Error; err != nil {
		return nil, false
	}
	return &s, true
}

// CreateSite 创建站点
func CreateSite(s *Site) error {
	return DB.Create(s).Error
}

// SaveSite 保存站点
func SaveSite(s *Site) error {
	return DB.Save(s).Error
}

// DeleteSite 删除站点
func DeleteSite(s *Site) error {
	return DB.Delete(s).Error
}

// UpdateSiteField 更新站点单个字段
func UpdateSiteField(id uint, field string, value interface{}) error {
	return DB.Model(&Site{}).Where("id = ?", id).Update(field, value).Error
}

// CountSites 统计站点数量
func CountSites() (int64, error) {
	var c int64
	err := DB.Model(&Site{}).Count(&c).Error
	return c, err
}

// --- SiteRedirect ---

// ListSiteRedirects 查询站点的重定向规则（按 sort 排序）
func ListSiteRedirects(siteID uint) ([]SiteRedirect, error) {
	var list []SiteRedirect
	err := DB.Where("site_id = ?", siteID).Order("sort asc, id asc").Find(&list).Error
	return list, err
}

// DeleteSiteRedirects 删除站点的全部重定向规则
func DeleteSiteRedirects(siteID uint) error {
	return DB.Where("site_id = ?", siteID).Delete(&SiteRedirect{}).Error
}

// CreateSiteRedirect 创建重定向规则
func CreateSiteRedirect(r *SiteRedirect) error {
	return DB.Create(r).Error
}
