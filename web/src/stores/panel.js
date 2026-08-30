import { defineStore } from 'pinia'
import { ref } from 'vue'
import { checkUpdate, upgradePanel } from '../api/update'

// 面板基础信息 store（名称、副标题、版本、更新状态等）。AdminLayout mount 时调用 loadPanelInfo 拉取。
export const usePanelStore = defineStore('panel', () => {
  const name = ref('开猿运维')
  const sub = ref('开猿运维')
  const version = ref('')
  const hasUpdate = ref(false)
  const updateVersion = ref('')
  const updateChangelog = ref('')
  const updateDownloadURL = ref('')
  const updateWebURL = ref('')
  const updateXdbURL = ref('')
  const updateForce = ref(false)
  const checking = ref(false)
  const upgrading = ref(false)

  function setInfo(info) {
    if (typeof info?.panel_name === 'string' && info.panel_name) name.value = info.panel_name
    if (typeof info?.panel_sub === 'string' && info.panel_sub) sub.value = info.panel_sub
    if (typeof info?.panel_version === 'string') version.value = info.panel_version
  }

  // force=true 时跳过后端缓存，立即向官网获取最新更新状态（手动检查更新时使用）
  async function checkForUpdate(force = false) {
    if (checking.value) return
    checking.value = true
    try {
      const { data } = await checkUpdate(undefined, force)
      hasUpdate.value = !!data?.has_update
      updateVersion.value = data?.version || ''
      updateChangelog.value = data?.changelog || ''
      updateDownloadURL.value = data?.download_url || ''
      updateWebURL.value = data?.web_url || ''
      updateXdbURL.value = data?.xdb_url || ''
      updateForce.value = !!data?.force
      if (typeof data?.current_version === 'string' && data.current_version) {
        version.value = data.current_version
      }
    } catch (e) {
      // 静默失败：网络不通时不影响正常使用
    } finally {
      checking.value = false
    }
  }

  async function doUpgrade() {
    if (!updateDownloadURL.value || upgrading.value) return
    upgrading.value = true
    try {
      await upgradePanel({
        download_url: updateDownloadURL.value,
        web_url: updateWebURL.value,
        xdb_url: updateXdbURL.value
      })
      return true
    } finally {
      // 升级命令已发出，后端会重启；前端是否设为 false 取决于轮询结果
      // 这里不重置，由调用方控制
    }
  }

  return {
    name,
    sub,
    version,
    hasUpdate,
    updateVersion,
    updateChangelog,
    updateDownloadURL,
    updateWebURL,
    updateXdbURL,
    updateForce,
    checking,
    upgrading,
    setInfo,
    checkForUpdate,
    doUpgrade
  }
})
