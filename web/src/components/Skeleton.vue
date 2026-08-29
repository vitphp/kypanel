<template>
  <div class="skeleton" :class="`skeleton--${type}`">
    <!-- 表格骨架：模拟 el-table 的表头 + N 行 -->
    <template v-if="type === 'table'">
      <div class="sk-table-head">
        <div v-for="(col, i) in columns" :key="i" class="sk-cell sk-shimmer" :style="{ width: col.width || 'auto', flex: col.flex || 1 }" />
      </div>
      <div v-for="r in rows" :key="r" class="sk-table-row">
        <div v-for="(col, i) in columns" :key="i" class="sk-cell sk-shimmer" :style="{ width: col.width || 'auto', flex: col.flex || 1 }" />
      </div>
    </template>

    <!-- 卡片骨架：模拟卡片网格 -->
    <template v-else-if="type === 'card'">
      <div v-for="r in rows" :key="r" class="sk-card">
        <div class="sk-card-head">
          <div class="sk-shimmer sk-avatar" />
          <div class="sk-card-title">
            <div class="sk-shimmer sk-line" style="width: 60%" />
            <div class="sk-shimmer sk-line" style="width: 35%" />
          </div>
        </div>
        <div class="sk-shimmer sk-line" style="width: 90%" />
        <div class="sk-shimmer sk-line" style="width: 75%" />
      </div>
    </template>

    <!-- 列表骨架：模拟行式列表（每行：标题 + 描述 + 操作按钮） -->
    <template v-else>
      <div v-for="r in rows" :key="r" class="sk-list-row">
        <div class="sk-list-main">
          <div class="sk-shimmer sk-line" style="width: 28%" />
          <div class="sk-shimmer sk-line" style="width: 50%" />
        </div>
        <div class="sk-shimmer sk-btn" />
      </div>
    </template>
  </div>
</template>

<script setup>
defineProps({
  // 骨架形态：table / card / list
  type: { type: String, default: 'list' },
  // 行数
  rows: { type: Number, default: 5 },
  // 表格列定义（仅 type=table 时有效）：[{ width: '120px' }, { flex: 1 }, ...]
  columns: { type: Array, default: () => [] }
})
</script>

<style scoped>
.skeleton { width: 100%; }

/* ===== shimmer 闪光动画 ===== */
.sk-shimmer {
  background: linear-gradient(90deg, #f0f2f5 25%, #e6e9ee 37%, #f0f2f5 63%);
  background-size: 400% 100%;
  animation: sk-flash 1.4s ease infinite;
  border-radius: 4px;
}
@keyframes sk-flash {
  0% { background-position: 100% 50%; }
  100% { background-position: 0 50%; }
}

/* ===== 通用线条 ===== */
.sk-line { height: 14px; margin: 6px 0; }
.sk-btn { width: 48px; height: 28px; flex-shrink: 0; }
.sk-avatar { width: 40px; height: 40px; border-radius: 8px; flex-shrink: 0; }

/* ===== 表格骨架 ===== */
.sk-table-head, .sk-table-row {
  display: flex; align-items: center; gap: 16px;
  padding: 0 16px;
}
.sk-table-head { height: 48px; border-bottom: 1px solid #ebeef5; background: #fafafa; }
.sk-table-row { height: 52px; border-bottom: 1px solid #f0f2f5; }
.sk-cell { height: 14px; }

/* ===== 卡片骨架 ===== */
.sk-card {
  padding: 16px; border: 1px solid #ebeef5; border-radius: 8px;
  margin-bottom: 12px; background: #fff;
}
.sk-card-head { display: flex; gap: 12px; margin-bottom: 14px; }
.sk-card-title { flex: 1; }

/* ===== 列表骨架 ===== */
.sk-list-row {
  display: flex; align-items: center; justify-content: space-between;
  padding: 14px 16px; border-bottom: 1px solid #f0f2f5;
}
.sk-list-main { flex: 1; }
</style>
