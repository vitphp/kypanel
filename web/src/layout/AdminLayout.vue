<template>
  <div
    class="lp-admin"
    :class="{
      'is-collapsed': collapsed,
      'is-mobile': viewport === 'mobile',
      'is-tablet': viewport === 'tablet',
      'is-desktop': viewport === 'desktop',
      'drawer-open': viewport === 'mobile' && mobileDrawerOpen
    }"
  >
    <TheTopbar :collapsed="collapsed" @toggle-collapse="toggleCollapse" />

    <div class="lp-body">
      <div v-if="viewport === 'mobile'" class="lp-sidebar-mask" :class="{ open: mobileDrawerOpen }" @click="mobileDrawerOpen = false"></div>
      <TheSidebar
        :collapsed="viewport !== 'mobile' && collapsed"
        class="lp-sidebar-wrap"
        :class="{ 'is-drawer': viewport === 'mobile' }"
        @toggle-collapse="viewport === 'mobile' ? (mobileDrawerOpen = !mobileDrawerOpen) : toggleCollapse()"
      />

      <main class="lp-main">
        <div class="lp-main-content">
          <router-view v-slot="{ Component, route: r }">
            <transition name="lp-fade" mode="out-in">
              <component :is="Component" :key="r.fullPath" />
            </transition>
          </router-view>
        </div>

        <!-- 全局页脚：放在主内容区域底部（不固定在屏幕最底） -->
        <footer class="lp-footer">
          <a :href="OFFICIAL_SITE" target="_blank" rel="noopener noreferrer" class="lp-footer-link">{{ panel.name }}</a> 版权所有 © 2026
        </footer>
      </main>
    </div>

    <!-- 全局浮窗终端：挂在 layout 层级，所有页面共享同一个终端实例 -->
    <FloatingTerminal />

    <!-- 全局右下角任务区：传输任务（上传/下载）+ 安装任务，横向排列 -->
    <TransferPanel />
    <InstallFloater />
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import TheTopbar from './TheTopbar.vue'
import TheSidebar from './TheSidebar.vue'
import FloatingTerminal from '../components/FloatingTerminal.vue'
import InstallFloater from '../components/InstallFloater.vue'
import TransferPanel from '../components/TransferPanel.vue'
import request from '../utils/request'
import { usePanelStore } from '../stores/panel'
import { OFFICIAL_SITE } from '../config'

const COLLAPSE_KEY = 'lp-sidebar-collapsed'

const collapsed = ref(false)
const viewport = ref('desktop') // 'mobile' | 'tablet' | 'desktop'
const mobileDrawerOpen = ref(false)

// 面板名称/副标题 store（顶栏 + 页脚版权共用）
const panel = usePanelStore()

function applyViewport() {
  const w = window.innerWidth
  if (w < 768) viewport.value = 'mobile'
  else if (w < 1200) viewport.value = 'tablet'
  else viewport.value = 'desktop'
  if (viewport.value !== 'mobile') mobileDrawerOpen.value = false
}

function toggleCollapse() {
  if (viewport.value === 'mobile') {
    mobileDrawerOpen.value = !mobileDrawerOpen.value
    return
  }
  collapsed.value = !collapsed.value
  localStorage.setItem(COLLAPSE_KEY, collapsed.value ? '1' : '0')
}

function onResize() {
  applyViewport()
}

onMounted(() => {
  const saved = localStorage.getItem(COLLAPSE_KEY)
  if (saved === '1') collapsed.value = true
  applyViewport()
  window.addEventListener('resize', onResize)
  // 拉取面板名称/副标题/版本（供 Topbar / 侧栏渲染）
  request.get('/settings/info').then((res) => {
    panel.setInfo(res.data)
    // 面板信息拉取成功后，顺便检查一次更新
    panel.checkForUpdate()
  }).catch(() => {})
})
onUnmounted(() => {
  window.removeEventListener('resize', onResize)
})
</script>

<style scoped>
.lp-admin {
  display: flex;
  flex-direction: column;
  height: 100vh;
  background: #f5f7fa;
  color: #0f172a;
}

.lp-body {
  flex: 1;
  display: flex;
  min-height: 0;
}

.lp-sidebar-wrap {
  position: relative;
  height: 100%;
  display: flex;
  flex-shrink: 0;
}

.lp-sidebar-mask {
  position: fixed;
  inset: 64px 0 0 0;
  background: rgba(15, 23, 42, 0.45);
  z-index: 40;
  opacity: 0;
  pointer-events: none;
  transition: opacity 0.2s;
}
.lp-sidebar-mask.open { opacity: 1; pointer-events: auto; }

/* 移动端：侧栏变 drawer */
.lp-admin.is-mobile .lp-sidebar-wrap.is-drawer {
  position: fixed;
  top: 64px;
  bottom: 0;
  left: 0;
  z-index: 50;
  width: 240px !important;
  transform: translateX(-100%);
  transition: transform 0.25s ease;
  box-shadow: 4px 0 16px rgba(0, 0, 0, 0.08);
}
.lp-admin.is-mobile.drawer-open .lp-sidebar-wrap.is-drawer { transform: translateX(0); }

.lp-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;            /* main 整体不滚动，让内部 lp-main-content 滚 */
}

/* 主内容滚动区：撑开剩余高度，自身滚动 */
.lp-main-content {
  flex: 1;
  min-height: 0;
  overflow: auto;
  padding: 16px;              /* 统一四周留白，与计划任务/数据库/监控页保持一致 */
}

.lp-footer {
  flex-shrink: 0;
  text-align: center;
  font-size: 12px;
  color: #94a3b8;
  line-height: 1;
  padding: 10px 16px;
  border-top: 1px solid #eef0f3;
  background: #f5f7fa;
}

.lp-footer-link {
  color: #64748b;
  text-decoration: none;
  transition: color 0.15s;
}
.lp-footer-link:hover {
  color: #475569;
  text-decoration: underline;
}

/* 窄屏（< 600px）去掉主内容区左右留白，并去掉 el-card 圆角，贴边显示充分利用手机屏幕宽度 */
@media (max-width: 599px) {
  .lp-main-content {
    padding: 0;
  }
  .lp-main-content :deep(.el-card) {
    border-radius: 0;
    border-left: 0;
    border-right: 0;
  }
  .lp-main-content :deep(.el-card.is-always-shadow),
  .lp-main-content :deep(.el-card.is-hover-shadow:hover) {
    box-shadow: none;
  }
}

/* 路由过渡 */
.lp-fade-enter-active, .lp-fade-leave-active { transition: opacity 0.15s, transform 0.15s; }
.lp-fade-enter-from { opacity: 0; transform: translateY(4px); }
.lp-fade-leave-to { opacity: 0; transform: translateY(-4px); }
</style>
