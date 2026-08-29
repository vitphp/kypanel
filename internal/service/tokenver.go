package service

import (
	"sync"

	"kypanel/internal/model"
)

// tokenVerCache 缓存 adminID → TokenVer，避免每次请求都查 DB。
// 改密/踢下线后通过 InvalidateTokenVer 清除对应缓存。
var (
	tokenVerMu    sync.RWMutex
	tokenVerCache = map[uint]int{}
)

// InvalidateTokenVer 清除指定管理员的 token 版本缓存（改密/踢下线后调用）
func InvalidateTokenVer(adminID uint) {
	tokenVerMu.Lock()
	delete(tokenVerCache, adminID)
	tokenVerMu.Unlock()
}

// TokenVerOf 返回管理员的当前 token 版本号（优先缓存，未命中查 DB）
func TokenVerOf(adminID uint) int {
	tokenVerMu.RLock()
	if v, ok := tokenVerCache[adminID]; ok {
		tokenVerMu.RUnlock()
		return v
	}
	tokenVerMu.RUnlock()

	var admin model.Admin
	if err := model.DB.First(&admin, adminID).Error; err != nil {
		return 0
	}
	tokenVerMu.Lock()
	tokenVerCache[adminID] = admin.TokenVer
	tokenVerMu.Unlock()
	return admin.TokenVer
}
