import { defineStore } from 'pinia'
import { ref } from 'vue'

const STORAGE_KEY = 'panel_file_tabs'
let seq = 1

function genId() {
  return 'ft_' + Date.now().toString(36) + '_' + seq++
}

function nameOf(p) {
  if (!p || p === '/') return '/'
  const parts = p.split('/').filter(Boolean)
  return parts[parts.length - 1] || '/'
}

// 把路径展开为祖先链（含自身），用于初始化前进/后退栈。
// 例：'/a/b/c' → ['/', '/a', '/a/b', '/a/b/c']
// 例：'E:/x/y' → ['E:/', 'E:/x', 'E:/x/y']
function ancestorsOf(p) {
  if (!p) return []
  const out = []
  let startFrom = 0
  if (p[0] === '/') {
    out.push('/')
    startFrom = 1
  } else if (p[1] === ':' && p[2] === '/') {
    out.push(p.substring(0, 3))
    startFrom = 3
  } else if (p[1] === ':') {
    out.push(p.substring(0, 2))
    startFrom = 2
  }
  for (let i = startFrom; i < p.length; i++) {
    if (p[i] === '/') out.push(p.substring(0, i))
  }
  out.push(p)
  return out
}

function loadPersisted() {
  try {
    const raw = JSON.parse(localStorage.getItem(STORAGE_KEY) || 'null')
    if (raw && Array.isArray(raw.tabs) && raw.tabs.length > 0) {
      const tabs = raw.tabs.map(t => ({
        id: t.id || genId(),
        path: t.path || '/',
        name: nameOf(t.path),
        // 恢复前进/后退栈；缺字段则用 [path] 单元素兜底
        history: Array.isArray(t.history) && t.history.length > 0 ? t.history.slice() : [t.path || '/'],
        historyIdx: Number.isInteger(t.historyIdx) ? t.historyIdx : 0
      }))
      return { tabs, activeId: raw.activeId || tabs[0].id }
    }
  } catch (e) { /* ignore */ }
  return null
}

// 多标签页状态：localStorage 持久化，刷新页面后恢复（含每个 tab 独立的前进/后退栈）
export const useFileTabsStore = defineStore('fileTabs', () => {
  const persisted = loadPersisted()
  const tabs = ref(persisted ? persisted.tabs : [{
    id: genId(),
    path: '/',
    name: '/',
    history: ['/'],
    historyIdx: 0
  }])
  const activeId = ref(persisted ? persisted.activeId : tabs.value[0].id)

  function persist() {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify({
        tabs: tabs.value.map(t => ({
          id: t.id,
          path: t.path,
          name: t.name,
          history: Array.isArray(t.history) ? t.history : [t.path],
          historyIdx: Number.isInteger(t.historyIdx) ? t.historyIdx : 0
        })),
        activeId: activeId.value
      }))
    } catch (e) { /* ignore */ }
  }

  // 当前激活标签（用于前进/后退栈）
  function getActiveTab() {
    return tabs.value.find(t => t.id === activeId.value) || tabs.value[0]
  }

  // 新增标签：已存在同路径则直接激活；新 tab 的 history 自动展开为祖先链
  // 这样从 URL 跳到 /www/wwwroot/testphp/permtestdir 时，
  // history = ['/www/wwwroot/testphp', '/www/wwwroot/testphp/permtestdir']，
  // 用户在 permtestdir 点后退能直接退回 testphp
  function addTab(path) {
    const p = path || '/'
    const found = tabs.value.find(t => t.path === p)
    if (found) {
      activeId.value = found.id
      persist()
      return found
    }
    const anc = ancestorsOf(p)
    const tab = {
      id: genId(),
      path: p,
      name: nameOf(p),
      history: anc,
      historyIdx: anc.length - 1
    }
    tabs.value.push(tab)
    activeId.value = tab.id
    persist()
    return tab
  }

  // 更新标签路径（跟随导航）
  function updateTabPath(id, path) {
    const t = tabs.value.find(t => t.id === id)
    if (t) {
      t.path = path
      t.name = nameOf(path)
      persist()
    }
  }

  // 更新指定 tab 的前进/后退栈（栈内容变化时调用，自动持久化）
  function updateTabHistory(id, history, historyIdx) {
    const t = tabs.value.find(t => t.id === id)
    if (!t) return
    t.history = history.slice()
    t.historyIdx = historyIdx
    persist()
  }

  // 关闭标签：至少保留一个
  function closeTab(id) {
    const idx = tabs.value.findIndex(t => t.id === id)
    if (idx < 0) return
    tabs.value.splice(idx, 1)
    if (tabs.value.length === 0) {
      tabs.value.push({
        id: genId(),
        path: '/',
        name: '/',
        history: ['/'],
        historyIdx: 0
      })
    }
    if (activeId.value === id) {
      activeId.value = tabs.value[Math.min(idx, tabs.value.length - 1)].id
    }
    persist()
  }

  return {
    tabs, activeId,
    getActiveTab,
    addTab, updateTabPath, updateTabHistory,
    closeTab, persist
  }
})
