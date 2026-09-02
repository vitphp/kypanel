<template>
  <div class="file-manager">
    <!-- 多标签栏 -->
    <div class="tab-bar">
      <div
        v-for="t in tabs.tabs"
        :key="t.id"
        class="tab-item"
        :class="{ active: t.id === tabs.activeId, closable: tabs.tabs.length > 1 }"
        @click="switchTab(t.id)"
        @mousedown.middle="closeTabHandler(t.id)"
        :title="t.path"
      >
        <el-icon class="tab-icon"><Folder /></el-icon>
        <span class="tab-name">{{ t.name }}</span>
        <i v-if="tabs.tabs.length > 1" class="tab-close" @click.stop="closeTabHandler(t.id)" title="关闭">×</i>
      </div>
      <div class="tab-add" @click="addTabPrompt" title="在新标签中打开目录">
        <el-icon><Plus /></el-icon>
      </div>
    </div>

    <!-- 工具栏：单行 flex + 响应式重排
         桌面 (≥1200px)：[←→↻] [路径] [搜索+按钮] [上传/下载] [新建] [回收站] [终端] 单行
         平板 (768–1199)：[←→↻] [路径] [搜索+按钮] 单行（搜索与刷新同一行）
                        [上传/下载] [新建] [回收站] [终端] 单行
         移动端 (<768px)：[搜索+按钮] [←→↻] 单行（搜索与刷新同一行）
                        [路径] 单行
                        [上传/下载] [新建] [回收站] [终端] 单行 -->
    <div class="toolbar">
      <!-- 组 1：前进 / 后退 / 刷新 + 地址栏（一个 div） -->
      <div class="tb-group tb-nav">
        <el-button-group>
          <el-button @click="goBack" :disabled="!canGoBack"><el-icon><Back /></el-icon></el-button>
          <el-button @click="goForward" :disabled="!canGoForward"><el-icon><Right /></el-icon></el-button>
          <el-button @click="refresh"><el-icon><Refresh /></el-icon></el-button>
        </el-button-group>
        <div ref="pathBarRef" class="path-bar" :class="{ 'is-editing': pathEditing }" @click="enterPathEdit">
          <template v-if="!pathEditing">
            <div class="path-crumb" :title="currentPath">
              <template v-for="(seg, i) in breadcrumbs" :key="seg.path">
                <span v-if="i > 0" class="crumb-sep">›</span>
                <button
                  class="crumb"
                  :class="{ 'is-last': i === breadcrumbs.length - 1 }"
                  :title="seg.path"
                  @click.stop="cd(seg.path)"
                >{{ i === 0 ? '根目录' : seg.name }}</button>
              </template>
            </div>
          </template>
          <template v-else>
            <input
              ref="pathEditInput"
              v-model="pathDraft"
              class="path-input"
              spellcheck="false"
              :placeholder="currentPath"
              @keydown="onPathInputKeydown"
              @blur="cancelPathEdit"
            />
          </template>
        </div>
      </div>

      <!-- 组 2：搜索框 + 搜索按钮（一个 div） -->
      <div class="tb-group tb-search">
        <el-input
          v-model="search"
          placeholder="在当前目录中搜索"
          clearable
          size="default"
          class="tb-search-input"
          @keyup.enter="onSearch"
          @clear="onSearch"
        />
        <el-button type="primary" class="tb-search-btn" @click="onSearch">
          <el-icon><Search /></el-icon>
          <span>搜索</span>
        </el-button>
      </div>

      <!-- 组 3：上传/下载 + 新建 + 剪贴板 + 回收站 + 终端（一个 div，8px 等距） -->
      <div class="tb-group tb-actions">
        <el-dropdown @command="onUploadCmd" trigger="click">
          <el-button type="primary"><el-icon><Upload /></el-icon>上传/下载</el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="upload-file"><el-icon><Document /></el-icon>上传文件</el-dropdown-item>
              <el-dropdown-item command="upload-dir"><el-icon><Folder /></el-icon>上传文件夹</el-dropdown-item>
              <el-dropdown-item command="remote-download" divided><el-icon><Link /></el-icon>远程下载(URL)</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <el-dropdown @command="onNewCmd" trigger="click">
          <el-button type="success"><el-icon><Plus /></el-icon>新建</el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="folder"><el-icon><Folder /></el-icon>新建文件夹</el-dropdown-item>
              <el-dropdown-item command="file"><el-icon><Document /></el-icon>新建文件</el-dropdown-item>
              <el-dropdown-item v-if="clip.hasItems" command="paste" divided>
                <el-icon><DocumentCopy /></el-icon>
                {{ clip.isCopy ? '粘贴(复制)' : '粘贴(移动)' }}到当前目录
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <el-button v-if="clip.hasItems" type="warning" plain @click="showClipboard = true">
          <el-icon><DocumentCopy /></el-icon>剪贴板 ({{ clip.count }})
        </el-button>
        <el-button @click="goTrash" plain><el-icon><Delete /></el-icon>回收站</el-button>
        <el-button @click="openTerminalWithCwd" plain>
          <el-icon class="terminal-icon"><svg viewBox="0 0 24 24" width="1em" height="1em" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 6 L10 12 L4 18"/><line x1="13" y1="18" x2="20" y2="18"/></svg></el-icon>终端
        </el-button>
      </div>

      <input ref="fileInputRef" type="file" multiple style="display:none" @change="onPickFiles($event, 'file')" />
      <input ref="dirInputRef" type="file" multiple webkitdirectory style="display:none" @change="onPickFiles($event, 'dir')" />
    </div>

    <!-- 剪贴板粘性状态条：提示已复制/剪切了哪些文件，可一键粘贴到当前目录 -->
    <div v-if="clip.hasItems" class="clipboard-bar" :class="clip.isCut ? 'is-cut' : 'is-copy'">
      <div class="cb-left">
        <el-icon size="20">
          <component :is="clip.isCut ? 'Scissor' : 'DocumentCopy'" />
        </el-icon>
        <span class="cb-title">
          {{ clip.isCopy ? '已复制' : '已剪切' }} {{ clip.count }} 项
          <span v-if="clip.sourceDir" class="cb-source">来自 {{ clip.sourceDir }}</span>
        </span>
        <el-tag v-if="clip.isCut" type="warning" size="small">移动</el-tag>
      </div>
      <div class="cb-right">
        <el-button type="primary" size="small" @click="pasteHere">
          <el-icon><DocumentCopy /></el-icon>粘贴到当前目录
        </el-button>
        <el-button size="small" @click="showClipboard = true">详情</el-button>
        <el-button size="small" @click="clip.clear()">取消</el-button>
      </div>
    </div>

    <!-- 搜索结果提示 -->
    <div v-if="searchMode" class="search-tip">
      搜索结果不支持排序功能{{ searchModeInZip ? '（zip 内）' : '' }}
    </div>

    <!-- 主内容区 -->
    <div ref="contentAreaRef" class="content-area" @dragenter.prevent="onDragEnter" @dragleave="onDragLeave" @dragover.prevent @drop.prevent="onDrop"
      @mousedown="onListMouseDown" @click="onTableClick">
      <!-- 拖选矩形框（Windows 风格框选） -->
      <div v-show="dragRectVisible" class="drag-sel-rect" :style="dragRectStyle"></div>
      <!-- 文件列表 -->
      <div v-if="!searchMode" class="file-grid" @click="onGridClick">
        <!-- 选中后浮在表头上的批量操作栏（覆盖整行表头，不再替换单列导致错位） -->
        <transition name="bulk-fade">
          <div v-show="selectedRows.length > 0" class="bulk-bar-overlay">
            <div class="bb-left">
              <el-checkbox :model-value="isAllSelected" :indeterminate="isPartialSelected" @change="toggleAll" />
              <span class="bb-text">已选 <b>{{ selectedRows.length }}</b> 项</span>
              <el-button class="bb-btn-inverse" size="small" link @click="inverseSelection">反选</el-button>
              <el-button class="bb-btn-cancel-select" size="small" type="warning" plain @click="clearSelection">
                <el-icon style="vertical-align: -2px; margin-right: 2px;"><CircleClose /></el-icon>取消选择
              </el-button>
            </div>
            <div class="bb-right">
              <el-button size="small" @click="batchCopy" title="复制 (Ctrl+C)"><el-icon><DocumentCopy /></el-icon><span class="bb-label">复制</span></el-button>
              <el-button size="small" @click="batchMove" title="移动 (Ctrl+X)"><el-icon><Promotion /></el-icon><span class="bb-label">移动</span></el-button>
              <el-button size="small" @click="batchCompress" title="压缩"><el-icon><Box /></el-icon><span class="bb-label">压缩</span></el-button>
              <el-button size="small" @click="batchPermission" title="权限"><el-icon><Setting /></el-icon><span class="bb-label">权限</span></el-button>
              <el-button size="small" type="danger" @click="batchDelete" title="删除 (Del)"><el-icon><Delete /></el-icon><span class="bb-label">删除</span></el-button>
            </div>
          </div>
        </transition>
        <el-table
          ref="tableRef"
          :data="pagedItems"
          row-key="path"
          v-loading="loading"
          @selection-change="onSelectionChange"
          @row-click="onRowClick"
          @row-dblclick="onRowDblClick"
          @row-contextmenu="onRowContextMenu"
          @sort-change="onSortChange"
          :row-class-name="rowClassName"
          stripe
          size="default"
        >
          <el-table-column type="selection" width="48" />
          <el-table-column label="名称" min-width="100" sortable="custom" :sort-orders="['ascending','descending',null]" prop="name">
            <template #default="{ row }">
              <div class="name-cell">
                <FileTypeIcon :name="row.name" :is-dir="row.is_dir" />
                <span v-if="renamingPath !== row.path" class="name-text" :title="row.name">{{ row.name }}</span>
                <el-input v-else v-model="nameValue" ref="renameInputRef" size="small" class="rename-input"
                  @keyup.enter="confirmRename(row)" @keyup.esc="cancelRename" @blur="confirmRename(row)" />
              </div>
            </template>
          </el-table-column>
          <el-table-column label="类型" width="80" sortable="custom" :sort-orders="['ascending','descending',null]" prop="is_dir">
            <template #default="{ row }">
              <span>{{ row.is_dir ? '目录' : '文件' }}</span>
            </template>
          </el-table-column>
          <el-table-column label="大小" width="110" prop="size" sortable="custom" :sort-orders="['ascending','descending',null]">
            <template #default="{ row }">
              <span>
                <template v-if="!row.is_dir">{{ formatBytes(row.size) }}</template>
                <template v-else>
                  <span v-if="duCache[row.path]" class="dir-size">{{ duCache[row.path] }}</span>
                  <el-button
                    v-else-if="computingDus.has(row.path)"
                    size="small"
                    link
                    class="dir-size-btn"
                    loading
                    @click.stop
                  >计算中…</el-button>
                  <el-button
                    v-else
                    size="small"
                    link
                    class="dir-size-btn"
                    :title="'计算 ' + row.path + ' 的实际磁盘占用'"
                    @click.stop="onCalcSize(row)"
                  >计算</el-button>
                </template>
              </span>
            </template>
          </el-table-column>
          <el-table-column label="权限" width="220">
            <template #default="{ row }">
              <span class="mode-cell" @click.stop="setPermission(row)" :title="row.user+':'+row.group + ' · 点击修改'">{{ row.mode }} / {{ row.user }}</span>
            </template>
          </el-table-column>
          <el-table-column label="修改时间" width="170" prop="mod_time" sortable="custom" :sort-orders="['ascending','descending',null]">
            <template #default="{ row }">
              <span>{{ row.mod_time }}</span>
            </template>
          </el-table-column>
          <el-table-column label="操作" min-width="140" fixed="right" align="right">
            <template #default="{ row }">
              <div class="row-actions">
                <el-button size="small" link type="primary" @click="openRow(row)">打开</el-button>
                <el-button v-if="row.is_dir" size="small" link type="primary" @click="openCompress([row])">压缩</el-button>
                <el-button v-if="!row.is_dir && row.name.toLowerCase().endsWith('.zip') && !isBackupZip(row.name)" size="small" link type="primary" @click="doDecompress(row)">解压</el-button>
                <el-button v-if="!row.is_dir && (!row.name.toLowerCase().endsWith('.zip') || isBackupZip(row.name))" size="small" link type="primary" @click="download(row)">下载</el-button>
                <el-dropdown @command="(c) => onMoreCmd(c, row)" trigger="click">
                  <el-button size="small" link type="primary" class="row-more-btn">更多<i class="el-icon-more-arrow"></i></el-button>
                  <template #dropdown>
                    <el-dropdown-menu>
                      <el-dropdown-item command="copy"><el-icon><DocumentCopy /></el-icon>复制</el-dropdown-item>
                      <el-dropdown-item command="cut"><el-icon><Scissor /></el-icon>移动</el-dropdown-item>
                      <el-dropdown-item v-if="clip.hasItems" command="paste-here">
                        <el-icon><DocumentCopy /></el-icon>粘贴到当前目录
                      </el-dropdown-item>
                      <el-dropdown-item v-if="row.is_dir" command="compress" divided>
                        <el-icon><Box /></el-icon>压缩
                      </el-dropdown-item>
                      <el-dropdown-item v-if="!row.is_dir && row.name.toLowerCase().endsWith('.zip') && isBackupZip(row.name)" command="decompress" divided>
                        <el-icon><Box /></el-icon>解压
                      </el-dropdown-item>
                      <el-dropdown-item command="rename" divided><el-icon><Edit /></el-icon>重命名</el-dropdown-item>
                      <el-dropdown-item command="permission"><el-icon><Setting /></el-icon>权限</el-dropdown-item>
                      <el-dropdown-item command="copy-path"><el-icon><Link /></el-icon>复制路径</el-dropdown-item>
                      <el-dropdown-item v-if="!row.is_dir" command="info" divided>
                        <el-icon><InfoFilled /></el-icon>文件信息
                      </el-dropdown-item>
                      <el-dropdown-item command="delete" divided><el-icon color="#f56c6c"><Delete /></el-icon><span style="color:#f56c6c">删除</span></el-dropdown-item>
                    </el-dropdown-menu>
                  </template>
                </el-dropdown>
              </div>
            </template>
          </el-table-column>
          <template #empty>
            <el-empty description="目录为空" class="file-empty">
              <template #description>
                <div>目录为空</div>
                <div class="empty-drop-hint">拖拽文件或文件夹到此处上传</div>
              </template>
            </el-empty>
          </template>
        </el-table>

        <!-- 右键菜单 -->
        <div
          v-show="contextMenu.visible"
          class="context-menu"
          :style="{ left: contextMenu.x + 'px', top: contextMenu.y + 'px' }"
          @click.stop
        >
          <div
            v-for="(item, idx) in contextMenuItems"
            :key="idx"
            class="context-menu-item"
            :class="{ divided: item.divided }"
            @click="onContextMenuItem(item)"
          >
            <el-icon class="context-menu-icon"><component :is="item.icon" /></el-icon>
            <span>{{ item.label }}</span>
          </div>
        </div>
      </div>

      <!-- 搜索结果 -->
      <div v-else class="search-results">
        <el-table :data="searchResults" v-loading="searchLoading" @row-click="onRowClick" @row-dblclick="onSearchRowDblClick">
          <el-table-column label="路径" min-width="300">
            <template #default="{ row }">
              <div class="result-row">
              <FileTypeIcon :name="row.name" :is-dir="row.is_dir" />
              <span class="result-name">{{ row.name }}</span>
              <span class="result-path">{{ row.path }}</span>
            </div>
            </template>
          </el-table-column>
          <el-table-column label="大小" width="100" :formatter="sizeFmt" prop="size" />
          <el-table-column label="操作" min-width="140" fixed="right" align="right">
            <template #default="{ row }">
              <div class="row-actions">
                <el-button size="small" link type="primary" @click="cd(row.dir)">打开目录</el-button>
                <el-button v-if="!row.is_dir && row.name.toLowerCase().endsWith('.zip') && !isBackupZip(row.name)" size="small" link type="primary" @click="doDecompress(row)">解压</el-button>
                <el-button v-if="!row.is_dir && (!row.name.toLowerCase().endsWith('.zip') || isBackupZip(row.name))" size="small" link type="primary" @click="download(row)">下载</el-button>
              </div>
            </template>
          </el-table-column>
          <template #empty>
            <el-empty :description="search ? '未找到匹配项' : '请输入搜索词'" />
          </template>
        </el-table>
      </div>
    </div>

    <!-- 拖拽遮罩 -->
    <div v-if="dragOver" class="drag-mask">
      <div class="drag-inner">
        <el-icon size="64"><Upload /></el-icon>
        <div class="drag-tip">松开以上传文件/文件夹到 <b>{{ currentPath }}</b></div>
      </div>
    </div>

    <!-- 上传同名文件冲突对话框 -->
    <el-dialog v-model="conflictVisible" title="文件已存在" width="440px" :close-on-click-modal="false" :close-on-press-escape="false" :show-close="false">
      <div class="conflict-body">
        <el-icon class="conflict-icon" :size="22"><WarningFilled /></el-icon>
        <div class="conflict-msg">
          目录中已存在同名文件 <b>{{ conflictName }}</b>，请选择处理方式：
        </div>
      </div>
      <template #footer>
        <el-button @click="conflictChoose('skipAll')">全部跳过</el-button>
        <el-button @click="conflictChoose('skip')">跳过</el-button>
        <el-button @click="conflictChoose('cancel')">取消</el-button>
        <el-button type="primary" plain @click="conflictChoose('overwriteAll')">全部覆盖</el-button>
        <el-button type="primary" @click="conflictChoose('overwrite')">覆盖</el-button>
      </template>
    </el-dialog>

    <!-- 分页 -->
    <div v-if="!searchMode && items.length > pageSize" class="pagination">
      <el-pagination
        v-model:current-page="page"
        :page-size="pageSize"
        :total="items.length"
        layout="prev, pager, next, jumper, total"
        background
      />
    </div>

    <!-- 权限对话框（完全复刻网站设置页的 3 卡样式） -->
    <el-dialog v-model="permVisible" title="修改权限" width="480px" @close="resetPerm">
      <el-form label-width="80px" label-position="left" class="perm-form">
        <el-form-item label="路径">
          <span class="perm-path">{{ permPath }}</span>
        </el-form-item>
        <el-form-item label="权限">
          <el-select
            v-model="permValue"
            class="perm-select"
            filterable
            allow-create
            default-first-option
            placeholder="如 755"
            @change="onPermValueChange"
          >
            <el-option
              v-for="opt in permPresets"
              :key="opt.value"
              :value="opt.value"
              :label="opt.label"
            />
          </el-select>
        </el-form-item>
        <el-form-item label="">
          <div class="perm-card-row">
            <div class="perm-card">
              <div class="perm-card-title">所有者</div>
              <el-checkbox v-model="permBits.oR">读</el-checkbox>
              <el-checkbox v-model="permBits.oW">写</el-checkbox>
              <el-checkbox v-model="permBits.oX">执行</el-checkbox>
            </div>
            <div class="perm-card">
              <div class="perm-card-title">属组</div>
              <el-checkbox v-model="permBits.gR">读</el-checkbox>
              <el-checkbox v-model="permBits.gW">写</el-checkbox>
              <el-checkbox v-model="permBits.gX">执行</el-checkbox>
            </div>
            <div class="perm-card">
              <div class="perm-card-title">其他</div>
              <el-checkbox v-model="permBits.xR">读</el-checkbox>
              <el-checkbox v-model="permBits.xW">写</el-checkbox>
              <el-checkbox v-model="permBits.xX">执行</el-checkbox>
            </div>
          </div>
        </el-form-item>
        <el-form-item label="属主">
          <el-row :gutter="10" class="perm-owner-row">
            <el-col :span="12">
              <el-select
                v-model="permOwner"
                filterable
                allow-create
                default-first-option
                clearable
                :loading="userListLoading"
                placeholder="用户"
                style="width: 100%;"
                popper-class="perm-owner-popper"
              >
                <el-option v-for="u in userList" :key="u" :label="u" :value="u" />
              </el-select>
            </el-col>
            <el-col :span="12">
              <el-select
                v-model="permGroup"
                filterable
                allow-create
                default-first-option
                clearable
                :loading="groupListLoading"
                placeholder="组"
                style="width: 100%;"
                popper-class="perm-owner-popper"
              >
                <el-option v-for="g in groupList" :key="g" :label="g" :value="g" />
              </el-select>
            </el-col>
          </el-row>
        </el-form-item>
        <el-form-item v-if="permIsDir" label="">
          <el-checkbox v-model="permRecursive">应用到子目录</el-checkbox>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="permVisible = false">取消</el-button>
        <el-button type="primary" @click="confirmPermission" :loading="permSaving">确定</el-button>
      </template>
    </el-dialog>

    <!-- 压缩对话框 -->
    <el-dialog v-model="compressVisible" :title="compressTitle" width="420px" @close="resetCompress">
      <el-form label-width="80px">
        <el-form-item label="源">
          <div class="compress-sources">
            <el-tag v-for="(s, i) in compressSources" :key="i" type="info" style="margin: 2px;">{{ s.name }}</el-tag>
          </div>
        </el-form-item>
        <el-form-item label="文件名">
          <el-input v-model="compressName" placeholder="如 backup" />
        </el-form-item>
        <el-form-item label="格式">
          <el-select v-model="compressFormat" style="width: 100%;">
            <el-option label="zip" value="zip" />
            <el-option label="tar.gz" value="tar.gz" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="compressVisible = false">取消</el-button>
        <el-button type="primary" @click="confirmCompress" :loading="compressDoing">开始压缩</el-button>
      </template>
    </el-dialog>

    <!-- 剪贴板详情对话框 -->
    <el-dialog v-model="showClipboard" title="文件剪贴板" width="560px">
      <el-alert :type="clip.isCopy ? 'info' : 'warning'" :closable="false" show-icon style="margin-bottom: 12px;">
        {{ clip.isCopy ? '复制模式：粘贴后会保留原文件' : '移动模式：粘贴后会删除原文件' }}
      </el-alert>
      <div class="clip-source">来源: {{ clip.sourceDir || '-' }}</div>
      <el-table :data="clipDetailRows" max-height="300" size="small">
        <el-table-column label="序号" type="index" width="60" />
        <el-table-column label="路径" min-width="300">
          <template #default="{ row }">{{ row }}</template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button @click="showClipboard = false">关闭</el-button>
        <el-button type="danger" plain @click="clip.clear()">清空剪贴板</el-button>
        <el-button type="primary" @click="pasteHere">粘贴到当前目录</el-button>
      </template>
    </el-dialog>

    <!-- 图片预览 -->
    <el-dialog v-model="previewVisible" :title="previewTitle" width="auto" top="5vh" align-center destroy-on-close>
      <div v-if="previewKind === 'image'" class="preview-image-wrap">
        <img :src="previewUrl" class="preview-image" />
      </div>
      <video v-else-if="previewKind === 'video'" :src="previewUrl" controls autoplay class="preview-media" />
      <audio v-else-if="previewKind === 'audio'" :src="previewUrl" controls autoplay class="preview-audio" />
    </el-dialog>

    <!-- 代码编辑器弹窗：URL 保持 /files，默认弹窗显示，可切换全屏 -->
    <el-dialog
      v-model="editorVisible"
      :fullscreen="editorFullscreen"
      :show-close="false"
      :close-on-click-modal="false"
      :close-on-press-escape="false"
      width="85%"
      class="file-editor-dialog"
      destroy-on-close
      @close="closeEditor"
    >
      <FileEditor
        :initial-path="editorInitialPath"
        :fullscreen="editorFullscreen"
        @close="closeEditor"
        @toggle-fullscreen="editorFullscreen = !editorFullscreen"
      />
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onBeforeUnmount, nextTick, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowDown, Back, Right, Refresh, Edit, EditPen, Upload, Document, Folder, Link, Plus, DocumentCopy, Delete, Promotion, Box, Setting, Scissor, InfoFilled, Search, Download, View, WarningFilled, CircleClose } from '@element-plus/icons-vue'
import request from '@/utils/request'
import { formatBytes as _formatBytes } from '@/utils/format'
import { useTransferStore, MAX_CHUNK_RETRIES, makeFileID } from '@/stores/transfer'
import { useClipboardStore } from '@/stores/clipboard'
import { useNavStore } from '@/stores/nav'
import { useFileTabsStore } from '@/stores/fileTabs'
import { useFloatingTerminalStore } from '@/stores/floatingTerminal'
import { isEditableFile, useFileEditorStore } from '@/stores/fileEditor'
import FileTypeIcon from '@/components/FileTypeIcon.vue'
import FileEditor from '@/views/FileEditor.vue'
import { parsePermToBits, buildPermFromBits, rwxToOctal } from '@/utils/perm'

// 判断 zip 是否为备份格式：原名_年月日_时分秒.zip
function isBackupZip(name) {
  return /^(.+)_\d{8}_\d{6}\.zip$/i.test(name)
}

const router = useRouter()
const transfer = useTransferStore()
const clip = useClipboardStore()
const nav = useNavStore()
const tabs = useFileTabsStore()
const floatTerm = useFloatingTerminalStore()

// ============= 基础状态 =============
const items = ref([])
const loading = ref(false)
const currentPath = ref('/root')
const search = ref('')
const searchMode = ref(false)
const searchModeInZip = ref(false)
const searchResults = ref([])
const searchLoading = ref(false)
const page = ref(1)
const pageSize = ref(50)

// ============= 排序状态 =============
// name / size / is_dir / mod_time；order: 'ascending' | 'descending' | null
// null 时按原始顺序（目录置顶 + 名称字典序）
const sortField = ref('')
const sortOrder = ref(null)

// 排序用：把目录视为「永远在文件前面」，同类型内部按用户指定字段排序
function compareItems(a, b, field, order) {
  const dirFirst = (b.is_dir ? 1 : 0) - (a.is_dir ? 1 : 0)
  if (dirFirst !== 0) return dirFirst
  let av, bv
  if (field === 'size') {
    av = a.is_dir ? -1 : (a.size ?? 0) // 目录排到该类型末位
    bv = b.is_dir ? -1 : (b.size ?? 0)
  } else if (field === 'is_dir') {
    av = a.is_dir ? 1 : 0
    bv = b.is_dir ? 1 : 0
  } else if (field === 'mod_time') {
    // mod_time 已是字符串，字典序等价时间序；空值排末
    av = a.mod_time || ''
    bv = b.mod_time || ''
  } else {
    // name 默认：本地化大小写不敏感字典序
    av = (a.name || '').toString()
    bv = (b.name || '').toString()
  }
  let cmp
  if (typeof av === 'number' && typeof bv === 'number') {
    cmp = av - bv
  } else {
    cmp = String(av).localeCompare(String(bv), undefined, { numeric: true, sensitivity: 'base' })
  }
  return order === 'descending' ? -cmp : cmp
}

// ============= 内嵌代码编辑器（弹窗，可切换全屏） =============
const editorVisible = ref(false)
const editorInitialPath = ref('')
const editorFullscreen = ref(false)

// ============= 文件夹大小计算缓存 =============
// 按目录路径缓存已计算的 du 结果；切换目录时清空，保持显示始终针对当前列表
const duCache = reactive({})             // { path: "1.5G" }
const computingDus = reactive(new Set()) // 当前正在计算的 path 集合（用于单元格 loading 态）

// ============= 上传 =============
const fileInputRef = ref(null)
const dirInputRef = ref(null)
const dragOver = ref(false)
let dragDepth = 0

// ============= 路径编辑态 =============
// 点面包屑右侧"笔"图标 → pathEditing=true，整条地址栏变成 input
// Enter 提交路径；Esc / 失焦 取消
const pathEditing = ref(false)
const pathDraft = ref('')
const pathEditInput = ref(null)
const pathBarRef = ref(null)
// 路径变化或窗口大小变化时，把路径栏滚到最右（显示当前目录）
function scrollPathBarToEnd() {
  const el = pathBarRef.value
  if (!el) return
  nextTick(() => { el.scrollLeft = el.scrollWidth })
}
watch(() => currentPath.value, scrollPathBarToEnd)
onMounted(() => {
  scrollPathBarToEnd()
  window.addEventListener('resize', scrollPathBarToEnd)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', scrollPathBarToEnd)
})
function enterPathEdit() {
  pathDraft.value = currentPath.value
  pathEditing.value = true
  nextTick(() => {
    const el = pathEditInput.value
    if (!el) return
    el.focus()
    el.select()
  })
}
function cancelPathEdit() {
  pathEditing.value = false
  pathDraft.value = ''
}
function commitPathEdit() {
  const v = (pathDraft.value || '').trim()
  if (!v) { cancelPathEdit(); return }
  // 保留分隔符在结构上的合法性（不以 / 开头的视为相对路径）
  const normalized = v.startsWith('/') ? v : '/' + v
  cancelPathEdit()
  cd(normalized)
}
function onPathInputKeydown(e) {
  if (e.key === 'Enter') { e.preventDefault(); commitPathEdit() }
  else if (e.key === 'Escape') { e.preventDefault(); cancelPathEdit() }
}

// ============= 重命名 =============
const renamingPath = ref('')
const nameValue = ref('')
const renameInputRef = ref(null)
// 防重入：Enter 提交后输入框销毁会再触发一次 blur/confirmRename，避免重复请求
let renameSaving = false

// ============= 权限 =============
const permVisible = ref(false)
const permPath = ref('')
const permValue = ref('')
const permOwner = ref('')
const permGroup = ref('')
// 常用权限预设（弹窗下拉用），755 放第一位
const permPresets = [
  { value: '755',  label: '755  rwxr-xr-x    (属主全部 / 组与其它 读+执行)' },
  { value: '644',  label: '644  rw-r--r--    (属主读写 / 组与其它 仅读)' },
  { value: '777',  label: '777  rwxrwxrwx    (全部可读写执行)' },
  { value: '700',  label: '700  rwx------    (仅属主)' },
  { value: '600',  label: '600  rw-------    (仅属主读写)' },
  { value: '664',  label: '664  rw-rw-r--    (属主与组读写 / 其它仅读)' },
  { value: '751',  label: '751  rwxr-x--x    (组读+执行 / 其它仅执行)' },
  { value: '775',  label: '775  rwxrwxr-x    (属主与组全部 / 其它 读+执行)' },
  { value: '744',  label: '744  rwxr--r--    (属主全部 / 组与其它 仅读)' },
  { value: '0',    label: '0    ---------    (无任何权限)' },
  { value: '7777', label: '7777  含 setuid / setgid / sticky 特殊位' }
]
const permBits = ref({
  oR: true, oW: true, oX: true,
  gR: true, gW: false, gX: true,
  xR: true, xW: false, xX: true
})
const permSaving = ref(false)
const permIsBatch = ref(false)
const permRecursive = ref(false)
const permIsDir = ref(false)
// 系统用户/用户组列表（供属主下拉选择）
const userList = ref([])
const groupList = ref([])
const userListLoading = ref(false)
const groupListLoading = ref(false)
let userGroupLoaded = false
async function loadUserGroups() {
  if (userGroupLoaded) return
  userListLoading.value = true
  groupListLoading.value = true
  try {
    const [uRes, gRes] = await Promise.all([
      request.get('/file/users').catch(() => ({ code: 1, data: [] })),
      request.get('/file/groups').catch(() => ({ code: 1, data: [] }))
    ])
    if (uRes && uRes.code === 0 && Array.isArray(uRes.data)) {
      userList.value = uRes.data.map((x) => x.name || x.Name || '').filter(Boolean)
      userGroupLoaded = true
    }
    if (gRes && gRes.code === 0 && Array.isArray(gRes.data)) {
      groupList.value = gRes.data.filter(Boolean)
      userGroupLoaded = true
    }
  } finally {
    userListLoading.value = false
    groupListLoading.value = false
  }
}

// ============= 压缩 =============
const compressVisible = ref(false)
const compressSources = ref([])
const compressName = ref('')
const compressFormat = ref('zip')
const compressDoing = ref(false)
const compressIsDir = ref(false)
const compressTitle = computed(() => compressIsDir.value ? '压缩目录' : '压缩文件')

// ============= 剪贴板详情 =============
const showClipboard = ref(false)
const clipDetailRows = computed(() => clip.paths.map((p) => p))

// ============= 选中 =============
const selectedRows = ref([])
const tableRef = ref(null)
const contentAreaRef = ref(null)
const dragRectVisible = ref(false)
const dragRectStyle = ref({})
let clickSelectTimer = null  // 单击选中延迟定时器，双击时取消
let pendingSelectRow = null  // 待延迟选中的行

// ============= 右键菜单 =============
const contextMenu = ref({ visible: false, x: 0, y: 0, row: null })
const contextMenuItems = ref([])

function buildContextMenu(row) {
  const items = []
  if (row.is_dir) {
    items.push({ label: '打开', icon: Folder, action: () => openDir(row) })
  } else {
    items.push({ label: '编辑', icon: Edit, action: () => openRow(row) })
    if (row.name.toLowerCase().endsWith('.zip') && !isBackupZip(row.name)) {
      items.push({ label: '解压', icon: Box, action: () => doDecompress(row) })
    } else {
      items.push({ label: '下载', icon: Download, action: () => download(row) })
    }
  }
  items.push({ label: '权限', icon: Setting, divided: true, action: () => setPermission(row) })
  items.push({ label: '复制', icon: DocumentCopy, action: () => doCopyRow(row) })
  items.push({ label: '剪切', icon: Scissor, action: () => doCutRow(row) })
  items.push({ label: '重命名', icon: EditPen, action: () => rename(row) })
  items.push({ label: '删除', icon: Delete, divided: true, action: () => doDelete(row) })
  if (!row.is_dir && row.name.toLowerCase().endsWith('.zip')) {
    items.push({ label: '解压', icon: Box, action: () => doDecompress(row) })
  } else {
    items.push({ label: '创建压缩', icon: Box, action: () => openCompress([row]) })
  }
  items.push({ label: '属性', icon: View, action: () => showInfo(row) })
  return items
}
// 多选文件时右键：批量操作菜单（复制/剪切/权限/压缩/删除）
function buildBatchContextMenu() {
  return [
    { label: '复制', icon: DocumentCopy, action: () => batchCopy() },
    { label: '剪切', icon: Scissor, action: () => batchMove() },
    { label: '权限', icon: Setting, action: () => batchPermission() },
    { label: '创建压缩', icon: Box, action: () => batchCompress() },
    { label: '删除', icon: Delete, action: () => batchDelete() },
  ]
}

function onRowContextMenu(row, column, event) {
  if (renamingPath.value === row.path) return
  event.preventDefault()
  event.stopPropagation()
  contextMenu.value.row = row
  // 多选（>=2）且右键的行在选中集合里 → 批量操作菜单；
  // 否则（未选中/单选/右键未选中行）显示单文件菜单
  const isMulti = selectedRows.value.length >= 2
  const rowSelected = selectedRows.value.some((r) => r.path === row.path)
  contextMenuItems.value = (isMulti && rowSelected)
    ? buildBatchContextMenu()
    : buildContextMenu(row)
  const menuWidth = 160
  const menuHeight = 280
  let x = event.clientX
  let y = event.clientY
  if (x + menuWidth > window.innerWidth) x = window.innerWidth - menuWidth - 8
  if (y + menuHeight > window.innerHeight) y = window.innerHeight - menuHeight - 8
  contextMenu.value.x = Math.max(8, x)
  contextMenu.value.y = Math.max(8, y)
  contextMenu.value.visible = true
}

function closeContextMenu() {
  contextMenu.value.visible = false
}

function onContextMenuItem(item) {
  closeContextMenu()
  if (item.action) item.action()
}

function onDocumentClick(e) {
  if (contextMenu.value.visible && !e.target.closest('.context-menu')) {
    closeContextMenu()
  }
  // 全局空白处取消表格选中：点击不在 file-grid 内、不在任何弹窗/dropdown 内、
  // 也不是工具栏/按钮/输入框等「有意义元素」时，清空选中。
  if (selectedRows.value.length === 0) return
  const t = e.target
  if (!t || !t.closest) return
  // 跳过所有弹窗、dropper、message-box、tooltip 等浮层（它们的内部点击不应清空表格选中）
  if (t.closest('.el-overlay, .el-overlay-dialog, .el-popper, .el-message-box, .el-drawer, .el-dialog, .el-tooltip__popper, .el-notification, .context-menu, .batch-bar')) return
  // 跳过工具栏 / 路径栏 / 标签栏 / 卡片 / 表单元素等（这些区域有自己的点击语义）
  if (t.closest('button, input, textarea, select, a, label, .el-checkbox, .el-switch, .el-input, .el-select, .lp-toolbar, .lp-path-bar, .lp-tabs, .lp-side, .el-menu, .el-dropdown, .el-pagination, .pagination-container, .el-card, .el-form, .el-radio-group, .el-checkbox-group')) return
  // 跳过文件网格本身（onGridClick 内部已有拖框抑制等精细处理，避免重复触发）
  if (t.closest('.file-grid')) return
  clearSelection()
}

function onKeyDown(e) {
  if (e.key === 'Escape') {
    closeContextMenu()
    // 焦点不在输入控件时按 ESC 也清空表格选中（输入控件里的 ESC 留给系统/业务方处理）
    const tag = (e.target?.tagName || '').toLowerCase()
    const isTyping = (tag === 'input' && e.target.type !== 'checkbox') || tag === 'textarea' || e.target?.isContentEditable
    if (!isTyping && selectedRows.value.length > 0) {
      clearSelection()
    }
    return
  }
  const t = e.target
  const tag = t ? (t.tagName || '').toLowerCase() : ''
  // 复选框的 input 不视为「正在输入」：勾选后按 Del 应直接删除选中项
  const isCheckboxInput = tag === 'input' && t.type === 'checkbox'
  const isTyping = (tag === 'input' && !isCheckboxInput) || tag === 'textarea' || (t && t.isContentEditable)
  // Ctrl/Cmd + C / X / V：复制 / 剪切 / 粘贴选中项（焦点在输入控件时不拦截，保留系统剪贴板行为）
  if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey) {
    const k = e.key.toLowerCase()
    if (!isTyping) {
      if (k === 'c' && selectedRows.value.length > 0) {
        e.preventDefault()
        batchCopy()
        return
      }
      if (k === 'x' && selectedRows.value.length > 0) {
        e.preventDefault()
        batchMove()
        return
      }
      if (k === 'v' && clip.hasItems) {
        e.preventDefault()
        pasteHere()
        return
      }
      if (k === 'a') {
        // Ctrl+A 全选当前列表：正常模式=当前页，搜索模式=全部搜索结果。
        // 逐行强制选中（幂等），避免 toggleAllSelection 在已全选时变成取消全选
        e.preventDefault()
        const list = searchMode.value ? searchResults.value : pagedItems.value
        list.forEach((r) => tableRef.value?.toggleRowSelection(r, true))
        return
      }
    }
    return
  }
  // Del 键：触发批量删除（需先选中，且焦点不在输入控件中）
  if (e.key === 'Delete' || e.key === 'Del') {
    if (isTyping) return
    if (selectedRows.value.length === 0) return
    e.preventDefault()
    batchDelete()
  }
}

const isAllSelected = computed(() => {
  if (pagedItems.value.length === 0) return false
  return pagedItems.value.every((r) => selectedRows.value.some((s) => s.path === r.path))
})
const isPartialSelected = computed(() => {
  return selectedRows.value.length > 0 && !isAllSelected.value
})

// ============= 预览 =============
const previewVisible = ref(false)
const previewUrl = ref('')
const previewTitle = ref('')
const previewKind = ref('image')

// ============= Computed =============
const canGoBack = computed(() => {
  const t = tabs.getActiveTab()
  return !!(t && Array.isArray(t.history) && t.historyIdx > 0)
})
const canGoForward = computed(() => {
  const t = tabs.getActiveTab()
  return !!(t && Array.isArray(t.history) && t.historyIdx < t.history.length - 1)
})
const pagedItems = computed(() => {
  const list = sortField.value && sortOrder.value
    ? [...items.value].sort((a, b) => compareItems(a, b, sortField.value, sortOrder.value))
    : items.value
  const start = (page.value - 1) * pageSize.value
  return list.slice(start, start + pageSize.value)
})

// 表格排序变化：记录状态 + 重置到第 1 页（避免排序后看的是某一页的片段）
function onSortChange({ prop, order }) {
  sortField.value = prop || ''
  sortOrder.value = order || null
  page.value = 1
}
const breadcrumbs = computed(() => {
  const parts = currentPath.value.split('/').filter(Boolean)
  const segs = [{ name: '/', path: '/' }]
  let acc = ''
  for (const p of parts) {
    acc += '/' + p
    segs.push({ name: p, path: acc })
  }
  // 如果当前路径就是根（只有 '/' 段），也保留 1 个根段
  return segs
})
const editTitle = computed(() => editPath.value || '编辑文件')
const editLangLabel = computed(() => editLang.value || 'text')

// ============= 浏览器前进/后退同步 =============
// 把当前目录同步到浏览器历史栈：用户点开文件夹时调用 pushState（点浏览器 back 时退到上一层目录）
// 首次进入 / 切 tab / 关 tab 时用 replaceState（不污染浏览器历史，只覆盖当前条目）
// 监听 window 的 popstate 事件，调用 listDir(path, false) 渲染目标目录
function pushBrowserHistory(path) {
  const tab = tabs.getActiveTab()
  if (!tab) return
  try { history.pushState({ path, tabId: tab.id }, '') } catch (e) { /* ignore */ }
}
function replaceBrowserHistory(path) {
  const tab = tabs.getActiveTab()
  if (!tab) return
  try { history.replaceState({ path, tabId: tab.id }, '') } catch (e) { /* ignore */ }
}
function onPopState(e) {
  const s = e && e.state
  if (!s || !s.path) return
  // 切回目标 tab（如果浏览器历史来自其他标签）
  if (s.tabId && s.tabId !== tabs.activeId) {
    const target = tabs.tabs.find((t) => t.id === s.tabId)
    if (target) {
      tabs.activeId = target.id
      tabs.persist()
    }
  }
  if (s.path !== currentPath.value) {
    listDir(s.path, false) // pushHistory=false：避免再 pushState 造成回环
  }
}

// ============= 工具函数 =============
function sizeFmt(row) {
  if (row.is_dir) return '-'
  return formatBytes(row.size)
}
function formatBytes(n) {
  return _formatBytes(n, { empty: '0 B' })
}

// ============= 文件夹大小计算（du -sb） =============
// 后端 /file/du 已存在，固定返回 ExecResult。stdout 是 "1234567\t/path"。
// -b 表示 --apparent-size：按真实字节、不乘块大小，空目录返回 0。
// 前端再用 formatBytes() 把它格式化成 "0 B / 1.5 GB"，跟表格里"大小"列规则一致。
function parseDuStdout(stdout) {
  if (!stdout) return ''
  const firstLine = String(stdout).trim().split(/\r?\n/)[0]
  const m = firstLine.match(/^(\d+)/) // 仅取数字段
  const bytes = m ? parseInt(m[1], 10) : NaN
  return Number.isFinite(bytes) ? formatBytes(bytes) : ''
}

async function onCalcSize(row) {
  if (!row || !row.path || computingDus.has(row.path)) return
  computingDus.add(row.path)
  try {
    const res = await request.get('/file/du', { params: { path: row.path } })
    if (res.code !== 0) {
      ElMessage.error(res.msg || '计算失败')
      return
    }
    const size = parseDuStdout(res.data?.stdout)
    if (size) duCache[row.path] = size
    else ElMessage.warning('未能解析 du 输出')
  } catch (e) {
    ElMessage.error('计算失败: ' + (e?.message || e))
  } finally {
    computingDus.delete(row.path)
  }
}

// 切目录时清理已不在当前列表中的 du 缓存，避免内存累积
function pruneDuCache() {
  const live = new Set(items.value.map((it) => it.path).filter(Boolean))
  for (const key of Object.keys(duCache)) {
    if (!live.has(key)) delete duCache[key]
  }
}
function getExt(name) {
  const i = name.lastIndexOf('.')
  return i >= 0 ? name.slice(i + 1).toLowerCase() : ''
}
function rowClassName({ row }) {
  return row.path === renamingPath.value ? 'is-renaming' : ''
}
async function fileOfUrl(path) {
  // 申请短时效预览 token（不泄露面板 JWT），拼成预览 URL
  try {
    const res = await request.post('/file/preview-token', { path })
    const token = res.data?.token
    if (!token) throw new Error('no token')
    return `/api/file/raw?path=${encodeURIComponent(path)}&token=${encodeURIComponent(token)}`
  } catch (e) {
    return ''
  }
}

// ============= 路径导航 =============
// 浏览器式历史栈：避免重复 push、避免层级式误判
// 规则：若目标路径已经在历史栈中存在，跳过去即可；不在再在末尾追加
// 把路径展开为祖先链（含自身），用于在 history 找不到祖先时初始化/重置栈
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

// 把指定路径同步到当前 tab 的前进/后退栈。
// push=true：用户主动切目录（openDir / cd / 首次挂载 / 外部带 path 进来），需要 push 或跳到已有位置
// push=false：前进/后退/刷新，只把 historyIdx 移到目标位置，不 push
// 每个 tab 独立维护，切换 tab 不影响其它 tab 的栈
function syncHistory(path, push = true) {
  const tab = tabs.getActiveTab()
  if (!tab) return
  const isAnc = (a, p) => {
    if (a === p) return true
    if (a === '/') return p.startsWith('/')
    return p.startsWith(a + '/')
  }
  // 防御：老数据 / 漏写 history 的 tab
  if (!Array.isArray(tab.history) || tab.history.length === 0) {
    const anc = ancestorsOf(path)
    tab.history = anc
    tab.historyIdx = anc.length - 1
    tabs.updateTabHistory(tab.id, tab.history, tab.historyIdx)
    return
  }
  if (!push) {
    // 前进/后退：目标通常已在 history 里
    const idx = tab.history.indexOf(path)
    if (idx >= 0) {
      if (idx !== tab.historyIdx) {
        tab.historyIdx = idx
        tabs.updateTabHistory(tab.id, tab.history, tab.historyIdx)
      }
    } else {
      // 不在 history 里（极端情况：history 与 path 不一致）→ 重置为祖先链
      const anc = ancestorsOf(path)
      tab.history = anc
      tab.historyIdx = anc.length - 1
      tabs.updateTabHistory(tab.id, tab.history, tab.historyIdx)
    }
    return
  }
  // push 模式
  const cur = tab.history[tab.historyIdx]
  if (cur === path) return
  const idx = tab.history.indexOf(path)
  if (idx >= 0) {
    // 目标在 history 里：跳到该位置（"点了历史项"）
    if (idx !== tab.historyIdx) {
      tab.historyIdx = idx
      tabs.updateTabHistory(tab.id, tab.history, tab.historyIdx)
    }
    return
  }
  // 找 history 中 path 的最近祖先，截断 forward 分支
  let newStack = tab.history.slice(0, tab.historyIdx + 1)
  let i = newStack.length - 1
  while (i >= 0 && !isAnc(newStack[i], path)) {
    i--
  }
  if (i < 0) {
    // history 里没有任何祖先与 path 相关（首次进入或跨分支）
    // 用祖先链重置，让用户在子目录可以逐步后退
    newStack = ancestorsOf(path)
  } else {
    // 保留祖先链
    newStack = newStack.slice(0, i + 1)
    newStack.push(path)
  }
  tab.history = newStack
  tab.historyIdx = newStack.length - 1
  tabs.updateTabHistory(tab.id, tab.history, tab.historyIdx)
}

async function listDir(path, pushHistory = true) {
  loading.value = true
  try {
    // silent：目录不存在等预期错误由本函数自行处理（静默关闭失效标签），不弹统一错误提示
    const res = await request.post('/file/list', { path }, { silent: true })
    items.value = res.data || []
    currentPath.value = path
    // 切换目录时，把上一级外的 du 缓存清掉，避免无关条目长期占用内存
    pruneDuCache()
    page.value = 1
    if (pushHistory) {
      syncHistory(path, true)
      pushBrowserHistory(path) // 同步到浏览器历史：点 back 时回到上一层目录
    } else {
      syncHistory(path, false)
    }
    // 同步 tab 信息
    tabs.updateTabPath(tabs.activeId, path)
  } catch (e) {
    if (isDirMissing(e)) {
      // 目录已被删除：静默移除对应标签，不弹报错
      removeMissingTab(path)
    } else {
      ElMessage.error('加载失败: ' + (e?.message || e))
    }
  } finally {
    loading.value = false
  }
}

// 判断是否为「目录不存在 / 不再是目录」这类预期错误（后端 os.ReadDir 的原生报错）
function isDirMissing(e) {
  const msg = e?.response?.data?.msg || e?.message || ''
  return /no such file or directory|not a directory/i.test(msg)
}

// 目录失效时：静默移除对应标签；若只剩最后一个标签则退回根目录，避免页面弹「文件夹不存在」
function removeMissingTab(path) {
  const idx = tabs.tabs.findIndex(t => t.path === path)
  const targetIdx = idx >= 0 ? idx : tabs.tabs.findIndex(t => t.id === tabs.activeId)

  // 只剩一个标签：不能关成空，退回根目录继续浏览
  if (tabs.tabs.length <= 1) {
    const first = tabs.tabs[0]
    if (first) {
      first.path = '/'
      first.name = '/'
      first.history = ['/']
      first.historyIdx = 0
      tabs.persist()
    }
    listDir('/', false)
    replaceBrowserHistory('/')
    return
  }

  if (targetIdx < 0) {
    listDir('/', false)
    return
  }

  const closing = tabs.tabs[targetIdx]
  const wasActive = closing.id === tabs.activeId
  tabs.closeTab(closing.id)
  if (wasActive) {
    const t = tabs.getActiveTab()
    if (t && t.path !== currentPath.value) {
      listDir(t.path, false)
    }
    if (t) replaceBrowserHistory(t.path)
  }
}
function deriveTabName(p) {
  if (!p || p === '/') return '/'
  return p.split('/').filter(Boolean).pop() || p
}
function cd(path) {
  listDir(path)
}
function refresh() {
  listDir(currentPath.value, false)
}
function goBack() {
  if (!canGoBack.value) return
  const tab = tabs.getActiveTab()
  if (!tab) return
  tab.historyIdx--
  const target = tab.history[tab.historyIdx]
  tabs.updateTabHistory(tab.id, tab.history, tab.historyIdx)
  listDir(target, false)
}
function goForward() {
  if (!canGoForward.value) return
  const tab = tabs.getActiveTab()
  if (!tab) return
  tab.historyIdx++
  const target = tab.history[tab.historyIdx]
  tabs.updateTabHistory(tab.id, tab.history, tab.historyIdx)
  listDir(target, false)
}
function goTrash() {
  router.push({ path: '/files/trash' })
}
function openTerminalWithCwd() {
  // 直接在当前页面弹出浮窗终端，cwd 即当前目录
  floatTerm.open({
    cwd: currentPath.value,
    title: currentPath.value || '终端'
  })
}

// ============= 行点击/双击 =============
// 行为约定（桌面端）：
//   - 单击行：只触发 el-table 自带的高亮选中（@row-click 不再进入/打开，避免误触）
//   - 双击行：文件夹→进入；文件→按类型预览或编辑器打开（@row-dblclick）
// 触屏设备：单击即打开（移动端 dblclick 不易触发，保留 isTouch 兜底）
// 打开文件夹：激活已有标签，否则在当前标签内前进（不新建标签）
// 300ms 内同一路径防重入：单击进目录后紧跟的双击（dblclick）不会重复请求
let lastOpenPath = ''
let lastOpenAt = 0
function openDir(row) {
  if (renamingPath.value === row.path) return
  const now = Date.now()
  if (row.path === lastOpenPath && now - lastOpenAt < 300) return
  lastOpenPath = row.path
  lastOpenAt = now
  const found = tabs.tabs.find((t) => t.path === row.path)
  if (found) {
    switchTab(found.id)
  } else {
    tabs.updateTabPath(tabs.activeId, row.path)
    listDir(row.path)
  }
}

// 单击行（桌面端）：不进入/打开，只让 el-table 自带的高亮选中接管。
// 统一约定：PC / 移动端都改为双击进文件夹、双击编辑/预览文件（与 PC 一致）。
// 触屏设备：移动端浏览器 dblclick 事件不可靠（双击常触发缩放），这里在 row-click 里
// 自行检测"两次 tap 间隔 < 350ms 且位置相近"视为双击，手动触发 onRowDblClick。
// event.detail：桌面单击 = 1；双击的第二次 click = 2（该次交给 dblclick 统一处理）
let lastTap = { path: '', x: 0, y: 0, t: 0 }
function onRowClick(row, column, event) {
  // 点击操作列按钮 / 复选框 / 输入框等交互元素时不拦截
  const t = event && event.target
  if (t && t.closest && t.closest('.row-actions, .el-checkbox, input, button, a')) return
  if (event.detail > 1) return
  // 仅触摸设备（pointer: coarse 或存在 ontouchstart）走双击检测，桌面保持原生 dblclick
  const isTouch = (typeof window !== 'undefined') &&
    (('ontouchstart' in window) || (window.matchMedia && window.matchMedia('(pointer: coarse)').matches))
  if (isTouch) {
    const now = Date.now()
    const x = (event.touches && event.touches[0] && event.touches[0].clientX)
      || (event.changedTouches && event.changedTouches[0] && event.changedTouches[0].clientX)
      || event.clientX
    const y = (event.touches && event.touches[0] && event.touches[0].clientY)
      || (event.changedTouches && event.changedTouches[0] && event.changedTouches[0].clientY)
      || event.clientY
    const near = Math.abs(x - lastTap.x) < 30 && Math.abs(y - lastTap.y) < 30
    if (lastTap.path === row.path && near && (now - lastTap.t) < 350) {
      // 视为双击：直接进入/打开
      lastTap = { path: '', x: 0, y: 0, t: 0 }
      onRowDblClick(row)
      return
    }
    lastTap = { path: row.path, x, y, t: now }
  }
  // 单击只负责选中，不再直接打开（与 PC 端行为一致；移动端也改为双击触发）
}

function onRowDblClick(row) {
  // 双击发生时立即取消延迟的单击选中，避免进入文件夹前先出现选中闪烁
  if (clickSelectTimer) {
    clearTimeout(clickSelectTimer)
    clickSelectTimer = null
  }
  pendingSelectRow = null
  window.getSelection()?.removeAllRanges()
  if (renamingPath.value === row.path) return
  if (row.is_dir) {
    // 双击文件夹同样进入（兼容桌面老习惯）；openDir 有 300ms 防重入，不会二次请求
    openDir(row)
  } else {
    openRow(row)
  }
}
async function openRow(row) {
  if (row.is_dir) {
    cd(row.path)
    return
  }
  const ext = getExt(row.name)
  // 图片预览
  if (['jpg','jpeg','png','gif','webp','bmp','svg','ico'].includes(ext)) {
    previewUrl.value = await fileOfUrl(row.path)
    previewTitle.value = row.name
    previewKind.value = 'image'
    previewVisible.value = true
    return
  }
  // 视频
  if (['mp4','webm','ogg','mov','m4v'].includes(ext)) {
    previewUrl.value = await fileOfUrl(row.path)
    previewTitle.value = row.name
    previewKind.value = 'video'
    previewVisible.value = true
    return
  }
  // 音频
  if (['mp3','wav','flac','aac','m4a','ogg'].includes(ext)) {
    previewUrl.value = await fileOfUrl(row.path)
    previewTitle.value = row.name
    previewKind.value = 'audio'
    previewVisible.value = true
    return
  }
  // 文本类打开多文件编辑器工作区（在当前 /files 路由内嵌，不跳转 URL）
  if (row.size <= 3 * 1024 * 1024) {
    if (!isEditableFile(row.name)) {
      ElMessage.info('暂不支持编辑该文件类型')
      return
    }
    editorInitialPath.value = row.path
    editorVisible.value = true
  } else {
    ElMessage.warning('文件超过 3MB，无法在线编辑')
  }
}
function closeEditor() {
  editorVisible.value = false
  editorInitialPath.value = ''
}

// ============= 选中 =============
function onSelectionChange(rows) {
  selectedRows.value = rows
}

// ============= 鼠标按下：单击选中 + 按住拖动范围批量选择 =============
// Windows 资源管理器风格：按下即选中当前行；按住左键拖动经过的行全部选中；
// 单击文件夹/文件只选中（不进入/打开），双击文件夹进入、双击文件编辑/预览。
let dragSel = null
// 拖框结束（mouseup）后会派发 click 到 file-grid，需抑制一次 click，避免 onGridClick 误清空刚选中的行
let suppressNextClick = false
function rowIndexFromEvent(e) {
  const t = e && e.target
  if (!t || !t.closest) return -1
  const tr = t.closest('.el-table__row')
  if (!tr || !tr.parentElement) return -1
  return Array.from(tr.parentElement.querySelectorAll('.el-table__row')).indexOf(tr)
}
function rowsOfClickTarget(t) {
  if (!t || !t.closest) return null
  if (t.closest('.file-grid')) return pagedItems.value
  if (t.closest('.search-results')) return searchResults.value
  return null
}
function onListMouseDown(e) {
  if (e.button !== 0) return
  // 正在重命名时直接放行：mousedown 的 preventDefault 会阻止焦点转移，
  // 使重命名输入框不失焦、@blur 不触发，导致点其他区域时改名内容被丢弃。
  if (renamingPath.value) return
  const t = e.target
  if (!t || !t.closest) return
  // 交互元素（复选框 / 操作列按钮 / 表头 / 输入框 / 右键菜单 / 空态）不拦截
  if (t.closest('.row-actions, .el-checkbox, input, button, a, .el-table__header-wrapper, .el-table__footer-wrapper, .context-menu, .el-table__empty-block')) return
  const rows = rowsOfClickTarget(t)
  if (!rows) return
  const idx = rowIndexFromEvent(e)
  e.preventDefault()

  // 空白处 mousedown：启动选区拖框（不动选中，等拖动超阈值再决定是清空还是累加）
  if (idx < 0) {
    const startIdx = findRowAt(e.clientX, e.clientY)
    dragSel = {
      startIdx, lastIdx: startIdx,
      moved: false,
      startX: e.clientX, startY: e.clientY, rows,
      startRow: null, isBlankDrag: true,
      baseSelection: new Set(selectedRows.value.map((r) => r.path)),
    }
    // 按下即显示框选矩形（Windows 资源管理器风格）
    dragRectVisible.value = true
    updateDragRect(e)
    document.addEventListener('mousemove', onDragSelMove)
    document.addEventListener('mouseup', onDragSelUp)
    return
  }

  const row = rows[idx]
  dragSel = {
    startIdx: idx, lastIdx: idx,
    moved: false,
    startX: e.clientX, startY: e.clientY, rows, startRow: row,
    isBlankDrag: false, baseSelection: null,
  }
  // 行内按下：选中状态变化会触发 Vue 重渲染，浏览器随后派发的 click 其 target 可能
  // 变成共同祖先（file-grid/表体）而匹配不上 onGridClick 的白名单 → 误清空。
  // 这里抑制紧随的这次 click（单击空白不设此标志，仍走"点空白取消选中"逻辑）
  suppressNextClick = true
  // 多选累加：点未选中行 → 加入；点已选中行 → 保留（避免误操作）；
  // 按住 Ctrl/Cmd → toggle；按住 Shift → 范围选；按住 Alt 不处理
  if (e.altKey) return
  if (e.shiftKey) {
    const last = selectedRows.value.length > 0
      ? selectedRows.value[selectedRows.value.length - 1]
      : null
    const lastIdx = last ? rows.findIndex((r) => r.path === last.path) : idx
    const a = Math.min(lastIdx, idx)
    const b = Math.max(lastIdx, idx)
    rows.forEach((r) => tableRef.value?.toggleRowSelection(r, false))
    rows.slice(a, b + 1).forEach((r) => tableRef.value?.toggleRowSelection(r, true))
  } else if (e.ctrlKey || e.metaKey) {
    // Ctrl/Cmd：toggle 当前行
    tableRef.value?.toggleRowSelection(row)
  } else {
    // 默认：点击行 toggle 选中状态，但延迟执行以区分双击。
    // 双击时 onRowDblClick 会清除定时器，从而避免"先选中再进入文件夹"的闪烁。
    if (clickSelectTimer) {
      clearTimeout(clickSelectTimer)
      clickSelectTimer = null
    }
    pendingSelectRow = row
    clickSelectTimer = setTimeout(() => {
      clickSelectTimer = null
      if (pendingSelectRow === row) {
        tableRef.value?.toggleRowSelection(row)
      }
      pendingSelectRow = null
    }, 220)
  }
  // 行内按下也显示框选矩形（Windows 风格：从行上按下拖动同样画框）
  dragRectVisible.value = true
  updateDragRect(e)
  document.addEventListener('mousemove', onDragSelMove)
  document.addEventListener('mouseup', onDragSelUp)
}
// Windows 风格框选矩形：从按下点画到当前鼠标位置，相对 content-area 定位
function updateDragRect(e) {
  if (!dragSel) return
  const cont = contentAreaRef.value
  if (!cont) return
  const r = cont.getBoundingClientRect()
  const sx = dragSel.startX - r.left
  const sy = dragSel.startY - r.top
  const cx = e.clientX - r.left
  const cy = e.clientY - r.top
  dragRectStyle.value = {
    left: Math.min(sx, cx) + 'px',
    top: Math.min(sy, cy) + 'px',
    width: Math.abs(cx - sx) + 'px',
    height: Math.abs(cy - sy) + 'px',
  }
}
// 用行元素 rect 判断鼠标当前所在行索引（比 elementFromPoint 更稳定，避开 fixed 克隆表干扰）
function findRowAt(x, y) {
  const tbodies = document.querySelectorAll('.el-table__body-wrapper table tbody')
  for (const tb of tbodies) {
    const rows = tb.querySelectorAll('.el-table__row')
    for (let i = 0; i < rows.length; i++) {
      const r = rows[i].getBoundingClientRect()
      if (r.height > 0 && y >= r.top && y <= r.bottom) return i
    }
  }
  return -1
}
// 拖选专用：超出表格上下边界时虚拟化到首/末行（向上超出→第一行；向下超出→最后一行），
// 保证向上/向下拖出文件列表外仍能选中（不再卡在边界行）
function findRowAtClamped(x, y) {
  const tbodies = document.querySelectorAll('.el-table__body-wrapper table tbody')
  for (const tb of tbodies) {
    const rows = tb.querySelectorAll('.el-table__row')
    if (rows.length === 0) continue
    const firstTop = rows[0].getBoundingClientRect().top
    const lastBottom = rows[rows.length - 1].getBoundingClientRect().bottom
    if (y < firstTop) return 0
    if (y > lastBottom) return rows.length - 1
    for (let i = 0; i < rows.length; i++) {
      const r = rows[i].getBoundingClientRect()
      if (r.height > 0 && y >= r.top && y <= r.bottom) return i
    }
    return -1
  }
  return -1
}
function onDragSelMove(e) {
  if (!dragSel) return
  // 矩形实时跟随鼠标（按下即画，随移动伸缩）
  updateDragRect(e)
  if (!dragSel.moved && Math.abs(e.clientX - dragSel.startX) + Math.abs(e.clientY - dragSel.startY) > 5) {
    dragSel.moved = true
    // 开始拖动 → 取消单击延迟选中，避免拖选结束时误触发单次点击的 toggle
    if (clickSelectTimer) {
      clearTimeout(clickSelectTimer)
      clickSelectTimer = null
      pendingSelectRow = null
    }
    if (dragSel.isBlankDrag && !e.ctrlKey && !e.metaKey && !e.shiftKey) {
      // 空白处开始拖动（默认行为）：先清空原选中
      tableRef.value?.clearSelection()
      dragSel.baseSelection = new Set()
    }
  }
  if (!dragSel.moved) return
  // 拖选用带边界虚拟化的查询：向上/向下拖出文件列表外时仍能选中（视为首/末行）
  const idx = findRowAtClamped(e.clientX, e.clientY)
  if (idx < 0) return // 鼠标完全脱离表格（极少见，正常不会发生）
  // 空白处按下时起点行未知（-1），进入第一行时以该行为起点
  if (dragSel.startIdx < 0) dragSel.startIdx = idx
  if (idx === dragSel.lastIdx) return
  dragSel.lastIdx = idx
  const rows = dragSel.rows
  // Windows 风格：选中范围 = 起点行 → 当前鼠标行（实时扩展/收缩，任意方向双向增减）
  const a = Math.min(dragSel.startIdx, idx)
  const b = Math.max(dragSel.startIdx, idx)
  const inRange = new Set(rows.slice(a, b + 1).map((r) => r.path))
  if (e.ctrlKey || e.metaKey) {
    // Ctrl/Cmd + 拖选：baseSelection 与拖选范围 XOR
    rows.forEach((r) => {
      const inBase = dragSel.baseSelection.has(r.path)
      const inDrag = inRange.has(r.path)
      const want = inBase ? !inDrag : inDrag
      tableRef.value?.toggleRowSelection(r, want)
    })
  } else {
    // 默认 / Shift：纯范围选（不在当前矩形内的行自动取消）
    rows.forEach((r) => tableRef.value?.toggleRowSelection(r, inRange.has(r.path)))
  }
}
function onDragSelUp(e) {
  document.removeEventListener('mousemove', onDragSelMove)
  document.removeEventListener('mouseup', onDragSelUp)
  dragRectVisible.value = false
  const d = dragSel
  dragSel = null
  // 空白框选：mouseup 回到起点附近（下方空白）→ 清空选择（之前在 mousemove 中处理
  // idx<0 会卡住，挪到此处判断，避免与"拖出表格外仍能选中"逻辑冲突）
  if (d && d.moved && d.isBlankDrag && e) {
    const lastRow = document.querySelector('.el-table__body-wrapper table tbody .el-table__row:last-child')
    if (lastRow) {
      const lastBottom = lastRow.getBoundingClientRect().bottom
      // 鼠标在下方空白，且与起点距离很小 → 视为"回到起点"
      if (e.clientY > lastBottom
        && Math.abs(e.clientY - d.startY) < 12
        && Math.abs(e.clientX - d.startX) < 12) {
        tableRef.value?.clearSelection()
      }
    }
  }
  // 真拖框（mousemove 超阈值）：mouseup 后浏览器会派发 click 到 file-grid，target 可能是
  // 矩形框（.drag-sel-rect）或表格空白，onGridClick 会被误触发清空，这里抑制一次。
  // 单击（!moved）不抑制，确保"点空白取消选中"行为不变。
  if (d && d.moved) {
    suppressNextClick = true
  }
}
// 点击复选框后让其失焦：避免焦点停留在 checkbox 的 input 上，导致按 Del / 空格被输入框逻辑拦截
function onTableClick(e) {
  const t = e.target
  if (!t || !t.closest) return
  const cb = t.closest('.el-checkbox')
  if (!cb) return
  const input = cb.querySelector('input')
  if (input) input.blur()
}
function toggleAll(val) {
  if (!tableRef.value) return
  if (val) {
    pagedItems.value.forEach((r) => tableRef.value.toggleRowSelection(r, true))
  } else {
    tableRef.value.clearSelection()
  }
}
function inverseSelection() {
  if (!tableRef.value) return
  const sel = new Set(selectedRows.value.map((r) => r.path))
  pagedItems.value.forEach((r) => {
    tableRef.value.toggleRowSelection(r, !sel.has(r.path))
  })
}
function clearSelection() {
  tableRef.value?.clearSelection()
}
// 点击 file-grid 空白处取消所有选中（行/复选框/按钮/工具栏/分页/右键菜单等"有意义"的元素不触发）
function onGridClick(e) {
  // 拖框结束的 mouseup 紧随其后派发 click 到 file-grid（target 可能是矩形或表格空白），
  // 这种 click 不应触发取消选中；单击空白（未拖动）会走原逻辑正常清空
  if (suppressNextClick) {
    suppressNextClick = false
    return
  }
  if (selectedRows.value.length === 0) return
  const t = e.target
  if (!t || !t.closest) return
  if (t.closest('.el-table__row, .row-actions, .el-checkbox, .el-table__header, button, input, a, .el-table__empty-block, .pagination-container, .el-pagination, .name-cell, .mode-cell, .el-dropdown, .el-popper, .context-menu, .batch-bar')) return
  clearSelection()
}

// ============= 复选框：批量操作 =============
async function batchCopy() {
  if (selectedRows.value.length === 0) return
  clip.setCopy(selectedRows.value.map((r) => r.path), currentPath.value)
  ElMessage.success(`已复制 ${selectedRows.value.length} 项，切换到目标目录后点击「粘贴到当前目录」`)
}
async function batchMove() {
  if (selectedRows.value.length === 0) return
  clip.setCut(selectedRows.value.map((r) => r.path), currentPath.value)
  ElMessage.success(`已剪切 ${selectedRows.value.length} 项，切换到目标目录后点击「粘贴到当前目录」`)
}
async function batchDelete() {
  if (selectedRows.value.length === 0) return
  const paths = selectedRows.value.map((r) => r.path)
  try {
    await ElMessageBox.confirm(`确定删除选中的 ${paths.length} 项？将进入回收站。`, '确认删除', { type: 'warning' })
  } catch { return }
  let ok = 0, fail = 0
  for (const p of paths) {
    try {
      const res = await request.post('/file/delete', { path: p })
      if (res.code === 0) ok++; else fail++
    } catch { fail++ }
  }
  ElMessage[fail === 0 ? 'success' : 'warning'](`完成：成功 ${ok}，失败 ${fail}`)
  refresh()
}
function formatTimestamp(d = new Date()) {
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}${pad(d.getMonth() + 1)}${pad(d.getDate())}_${pad(d.getHours())}${pad(d.getMinutes())}${pad(d.getSeconds())}`
}

function openCompress(sources) {
  if (!sources || sources.length === 0) return
  compressSources.value = sources.map((r) => ({ name: r.name, path: r.path }))
  compressIsDir.value = sources.length === 1 ? sources[0].is_dir : false
  compressName.value = (sources.length === 1 ? sources[0].name.replace(/\\.[^.]+$/, '') : 'archive') + '_' + formatTimestamp()
  compressFormat.value = 'zip'
  compressVisible.value = true
}

async function batchCompress() {
  openCompress(selectedRows.value)
}

// ============= 单行「更多」下拉命令 =============
function onMoreCmd(cmd, row) {
  switch (cmd) {
    case 'copy': doCopyRow(row); break
    case 'cut': doCutRow(row); break
    case 'paste-here': pasteHere(); break
    case 'rename': rename(row); break
    case 'permission': setPermission(row); break
    case 'copy-path': doCopyPath(row); break
    case 'compress': openCompress([row]); break
    case 'decompress': doDecompress(row); break
    case 'info': showInfo(row); break
    case 'delete': doDelete(row); break
  }
}
function doCopyRow(row) {
  clip.setCopy([row.path], currentPath.value)
  ElMessage.success(`已复制 1 项，切换到目标目录后点击「粘贴到当前目录」`)
}
function doCutRow(row) {
  clip.setCut([row.path], currentPath.value)
  ElMessage.success(`已剪切 1 项，切换到目标目录后点击「粘贴到当前目录」`)
}
function doCopyPath(row) {
  try {
    navigator.clipboard.writeText(row.path)
    ElMessage.success('路径已复制')
  } catch {
    ElMessage.warning('复制失败：浏览器不支持')
  }
}
async function doDecompress(row) {
  // 解压会直接写入目标目录，同名文件会被覆盖且无法恢复，操作前必须让用户确认
  const esc = (s) =>
    String(s ?? '').replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]))
  try {
    await ElMessageBox.confirm(
      `压缩包：${esc(row.name)}<br>解压到：${esc(currentPath.value)}<br><br>` +
        `<strong>若压缩包内的文件与该目录下已有同名文件冲突，将被直接覆盖且无法恢复。</strong>`,
      '确认解压',
      {
        type: 'warning',
        confirmButtonText: '确定解压',
        cancelButtonText: '取消',
        dangerouslyUseHTMLString: true
      }
    )
  } catch {
    return
  }
  try {
    const res = await request.post('/file/unzip', {
      path: row.path,
      dest_dir: currentPath.value
    })
    if (res.code !== 0) {
      ElMessage.error(res.msg || '解压失败')
      return
    }
    ElMessage.success('解压完成')
    refresh()
  } catch (e) {
    ElMessage.error('解压失败: ' + (e?.message || e))
  }
}
function showInfo(row) {
  ElMessageBox.alert(
    `路径: ${row.path}\n类型: ${row.is_dir ? '目录' : '文件'}\n大小: ${formatBytes(row.size)}\n权限: ${row.mode}\n属主: ${row.user}:${row.group}\n修改时间: ${row.mod_time}`,
    '文件信息',
    { confirmButtonText: '关闭' }
  )
}
async function doDelete(row) {
  try {
    await ElMessageBox.confirm(`确定删除「${row.name}」？将进入回收站。`, '确认删除', { type: 'warning' })
  } catch { return }
  try {
    const res = await request.post('/file/delete', { path: row.path })
    if (res.code !== 0) {
      ElMessage.error(res.msg || '删除失败')
      return
    }
    ElMessage.success('已删除')
    refresh()
  } catch (e) {
    ElMessage.error('删除失败: ' + (e?.message || e))
  }
}

// ============= 粘贴 =============
async function pasteHere() {
  if (!clip.hasItems) {
    ElMessage.warning('剪贴板为空')
    return
  }
  const dest = currentPath.value
  // 校验：复制/移动到自身或其子目录需要拒绝
  for (const p of clip.paths) {
    if (p === dest) {
      ElMessage.error(`源与目标相同：${p}`)
      return
    }
    if (dest.startsWith(p + '/')) {
      ElMessage.error(`不能复制/移动到自身子目录：${p} -> ${dest}`)
      return
    }
  }
  let ok = 0, fail = 0, failMsg = ''
  for (const p of clip.paths) {
    try {
      if (clip.isCopy) {
        const res = await request.post('/file/copy', { path: p, dest_dir: dest })
        if (res.code === 0) ok++; else { fail++; if (!failMsg) failMsg = res.msg }
      } else {
        const res = await request.post('/file/mv', { path: p, dest_dir: dest })
        if (res.code === 0) ok++; else { fail++; if (!failMsg) failMsg = res.msg }
      }
    } catch (e) {
      fail++
    }
  }
  if (fail === 0) {
    ElMessage.success(`${clip.isCopy ? '复制' : '移动'}完成：${ok} 项`)
    if (clip.isCut) clip.clear()
  } else {
    ElMessage.error(`完成：成功 ${ok}，失败 ${fail}${failMsg ? '（' + failMsg + '）' : ''}`)
  }
  refresh()
}

// ============= 重命名 =============
function rename(row) {
  renamingPath.value = row.path
  nameValue.value = row.name
  nextTick(() => {
    const el = renameInputRef.value?.$el?.querySelector('input') || renameInputRef.value?.$el
    if (el && el.focus) {
      el.focus()
      // 选中文件名主体（不含扩展名）
      const dot = row.name.lastIndexOf('.')
      if (!row.is_dir && dot > 0) {
        try { el.setSelectionRange(0, dot) } catch {}
      } else {
        try { el.select() } catch {}
      }
    }
  })
}
async function confirmRename(row) {
  if (renameSaving) return
  const newName = (nameValue.value || '').trim()
  if (!newName || newName === row.name) {
    cancelRename()
    return
  }
  // 校验非法字符
  if (/[\/\\<>:"|?*\x00-\x1f]/.test(newName)) {
    ElMessage.error('文件名包含非法字符')
    return
  }
  // 用文件自身所在目录计算新路径：搜索结果等场景下 currentPath 可能不是文件所在目录，
  // 直接用 currentPath 会把文件"改名"到别的目录（等同移动）。
  const dir = row.path.slice(0, row.path.lastIndexOf('/'))
  const newPath = dir + '/' + newName
  renameSaving = true
  try {
    const res = await request.post('/file/rename', { old_path: row.path, new_path: newPath })
    if (res.code !== 0) {
      ElMessage.error(res.msg || '重命名失败')
      return
    }
    ElMessage.success('已重命名')
    cancelRename()
    refresh()
  } catch (e) {
    ElMessage.error('重命名失败: ' + (e?.message || e))
  } finally {
    renameSaving = false
  }
}
function cancelRename() {
  renamingPath.value = ''
  nameValue.value = ''
}

// ============= 新建 =============
async function onNewCmd(cmd) {
  if (cmd === 'paste') {
    pasteHere()
    return
  }
  const isDir = cmd === 'folder'
  let def = isDir ? 'new_folder' : 'new_file.txt'
  let input
  try {
    const r = await ElMessageBox.prompt('请输入名称', isDir ? '新建文件夹' : '新建文件', {
      inputValue: def,
      inputValidator: (v) => (v && v.trim() ? true : '名称不能为空')
    })
    input = r.value.trim()
  } catch { return }
  if (/[\/\\<>:"|?*\x00-\x1f]/.test(input)) {
    ElMessage.error('名称包含非法字符')
    return
  }
  const path = currentPath.value.replace(/\/+$/, '') + '/' + input
  try {
    const res = await request.post('/file/create', { path, is_dir: isDir })
    if (res.code !== 0) {
      ElMessage.error(res.msg || '创建失败')
      return
    }
    ElMessage.success('创建成功')
    refresh()
  } catch (e) {
    ElMessage.error('创建失败: ' + (e?.message || e))
  }
}

// ============= 上传命令 =============
function onUploadCmd(cmd) {
  if (cmd === 'upload-file') {
    fileInputRef.value?.click()
  } else if (cmd === 'upload-dir') {
    dirInputRef.value?.click()
  } else if (cmd === 'remote-download') {
    doRemoteDownload()
  }
}
async function doRemoteDownload() {
  let r
  try {
    r = await ElMessageBox.prompt('请输入远程 URL（http/https）', '远程下载', {
      inputPlaceholder: 'https://example.com/file.zip',
      inputValidator: (v) => {
        if (!v) return 'URL 不能为空'
        if (!/^https?:\/\//i.test(v)) return 'URL 必须以 http:// 或 https:// 开头'
        return true
      }
    })
  } catch { return }
  try {
    const res = await request.post('/file/remote_download', { url: r.value, path: currentPath.value })
    if (res.code !== 0) {
      ElMessage.error(res.msg || '下载失败')
      return
    }
    // 异步任务：进入右下角传输队列（远程下载），前端轮询后端进度
    ElMessage.success('已加入远程下载队列')
    startRemoteDownloadPolling()
  } catch (e) {
    ElMessage.error('下载失败: ' + (e?.message || e))
  }
}

// 轮询后端远程下载任务进度，同步到 transfer 队列
let remoteDlPollingTimer = null
function startRemoteDownloadPolling() {
  if (remoteDlPollingTimer) return
  const sync = async () => {
    let hasActive = false
    try {
      const res = await request.get('/file/remote_download/tasks')
      const list = res.data || []
      // 同步到 transfer 队列：以「任务 ID」为 key，type 用 'download' 但加 remote 标记
      for (const t of list) {
        const key = 'remote_' + t.id
        const existing = transfer.tasks.find((x) => x.remoteId === t.id)
        const status = t.status === 'done' ? 'done' : (t.status === 'failed' ? 'error' : 'running')
        if (t.status === 'downloading') hasActive = true
        if (existing) {
          existing.total = t.total > 0 ? t.total : existing.total
          existing.loaded = t.loaded
          existing.speed = t.speed || 0
          existing.status = status
          existing.error = t.error || ''
        } else {
          const taskId = transfer.addTask({
            type: 'download',
            name: t.name,
            dir: t.dir,
            total: t.total > 0 ? t.total : 0,
            remoteId: t.id
          })
          // 补齐字段
          const nt = transfer.tasks.find((x) => x.id === taskId)
          if (nt) {
            nt.loaded = t.loaded
            nt.status = status
            nt.speed = t.speed || 0
            if (t.error) nt.error = t.error
          }
        }
      }
      // 后端已完成并清理的任务，前端也同步移除（避免残留）
      const activeIds = new Set(list.map((t) => 'remote_' + t.id))
      for (const t of [...transfer.tasks]) {
        if (t.remoteId && !activeIds.has(t.remoteId) && t.status === 'done') {
          // 已完成且后端已清理，保留 1.5s 让用户看到"完成"后自动移除
        }
      }
    } catch (e) { /* 忽略网络波动 */ }
    if (hasActive) {
      remoteDlPollingTimer = setTimeout(sync, 1000)
    } else {
      remoteDlPollingTimer = null
    }
  }
  remoteDlPollingTimer = setTimeout(sync, 200)
}

function onPickFiles(e, mode) {
  const files = Array.from(e.target.files || [])
  e.target.value = ''
  if (files.length === 0) return
  uploadFiles(files, mode === 'dir')
}

async function uploadFiles(list, isDirUpload) {
  // 每次批量选择文件时重置"全部覆盖/全部跳过"记忆
  overwriteAll.value = false
  skipAll.value = false
  for (const item of list) {
    const file = item.file || item
    const relPath = item.relativePath || item.webkitRelativePath || item._relativePath || ''
    let subPath = ''
    if (relPath) {
      // 后端根据 sub_path 重建目录
      const idx = relPath.lastIndexOf('/')
      if (idx > 0) subPath = relPath.slice(0, idx)
    }
    // 统一字节级续传上传：小文件 offset=0 一次直传，大文件断点续传
    startByteUpload({ file, name: relPath || file.name, dir: currentPath.value, subPath })
  }
}

// ============= 同名文件冲突处理 =============
const overwriteAll = ref(false)
const skipAll = ref(false)
const conflictVisible = ref(false)
const conflictName = ref('')
let conflictResolve = null

function openConflictDialog(name) {
  return new Promise((resolve) => {
    conflictName.value = name
    conflictVisible.value = true
    conflictResolve = resolve
  })
}

function conflictChoose(action) {
  conflictVisible.value = false
  const resolve = conflictResolve
  conflictResolve = null
  if (action === 'overwriteAll') overwriteAll.value = true
  if (action === 'skipAll') skipAll.value = true
  resolve?.(action)
}

/**
 * 处理目标文件已存在时的冲突
 * @returns 'overwrite' | 'skip' | 'cancel'
 */
async function resolveConflict(name) {
  if (overwriteAll.value) return 'overwrite'
  if (skipAll.value) return 'skip'
  return openConflictDialog(name)
}

/**
 * 统一上传（字节级断点续传）：无论文件大小，都走同一个 /file/upload 接口。
 * - 先探测服务端已写字节数 offset，从断点继续（断点续传）
 * - 小文件 offset=0，一次直传，行为等价于旧的单文件上传
 * - 服务端始终只有一个临时文件 <dst>.up.<file_id>.part，完成时重命名为最终文件
 */
function startByteUpload({ file, name, dir, subPath }) {
  const filename = (name || file.name).split('/').pop()
  const fileID = makeFileID(file)

  // 串行队列：初始状态为 pending，由 transferQueueTick 调度为 running 后才真正开始
  const taskId = transfer.addTask({
    type: 'upload',
    name,
    dir,
    total: file.size,
    fileID,
    status: 'pending'
  })

  transfer.registerController(taskId, new AbortController())

  // 单次续传请求，失败按指数退避重试至 MAX_CHUNK_RETRIES 次
  const uploadSlice = (blob, offset) => {
    return new Promise((resolve) => {
      let retried = 0
      const token = localStorage.getItem('panel_token') || ''
      const signal = transfer.tasks.find((t) => t.id === taskId)?.controller?.signal || null

      const tryOnce = () => {
        const tNow = transfer.tasks.find((t) => t.id === taskId)
        if (!tNow) { resolve('canceled'); return }
        if (tNow.paused) { resolve('paused'); return }
        if (signal && signal.aborted) { resolve('canceled'); return }

        const fd = new FormData()
        // 元数据字段必须放在 file 之前：后端用 MultipartReader 流式读，
        // 先读元数据确定临时文件路径，最后读 file part 边写边落盘（大文件实时变大）
        fd.append('path', dir)
        if (subPath) fd.append('sub_path', subPath)
        fd.append('filename', filename)
        fd.append('file_id', fileID)
        fd.append('offset', String(offset))
        fd.append('total_size', String(file.size))
        fd.append('file', blob, filename)

        const xhr = new XMLHttpRequest()
        let aborted = false
        const onAbort = () => {
          if (aborted) return
          aborted = true
          try { xhr.abort() } catch (e) {}
        }
        if (signal) signal.addEventListener('abort', onAbort, { once: true })

        xhr.open('POST', '/api/file/upload', true)
        xhr.setRequestHeader('Authorization', 'Bearer ' + token)
        xhr.upload.onprogress = (ev) => {
          if (!ev.lengthComputable) return
          transfer.updateTask(taskId, Math.min(offset + ev.loaded, file.size))
        }
        xhr.onload = () => {
          if (aborted) return
          if (signal) signal.removeEventListener('abort', onAbort)
          let res
          try { res = JSON.parse(xhr.responseText) } catch { res = null }
          if (xhr.status >= 200 && xhr.status < 300 && res && res.code === 0) {
            resolve({ complete: !!res.data?.complete, offset: res.data?.offset || offset })
          } else if (xhr.status === 0) {
            // abort / 网络失败
            if (++retried > MAX_CHUNK_RETRIES) {
              transfer.setTaskStatus(taskId, 'error', '上传失败（重试耗尽）')
              resolve(false)
            } else {
              setTimeout(tryOnce, 800 * retried)
            }
          } else {
            const msg = res?.msg || ('HTTP ' + xhr.status)
            // 5xx 才重试，4xx 直接报错
            if (xhr.status >= 500 && ++retried <= MAX_CHUNK_RETRIES) {
              setTimeout(tryOnce, 800 * retried)
            } else {
              transfer.setTaskStatus(taskId, 'error', msg)
              resolve(false)
            }
          }
        }
        xhr.onerror = () => {
          if (aborted) return
          if (signal) signal.removeEventListener('abort', onAbort)
          if (++retried > MAX_CHUNK_RETRIES) {
            transfer.setTaskStatus(taskId, 'error', '网络错误')
            resolve(false)
          } else {
            setTimeout(tryOnce, 800 * retried)
          }
        }
        xhr.onabort = () => {
          if (aborted) return
          const tn = transfer.tasks.find((t) => t.id === taskId)
          if (tn && tn.paused) resolve('paused')
          else resolve('canceled')
        }
        xhr.send(fd)
      }
      tryOnce()
    })
  }

  const startFn = async () => {
    try {
      // 探测服务端已写字节数（断点续传命中）
      let offset = 0
      let exists = false
      try {
        const r = await request.get('/file/upload/offset', {
          params: {
            path: dir,
            sub_path: subPath,
            filename,
            file_id: fileID,
            total_size: file.size
          }
        })
        if (r && r.code === 0) {
          offset = r.data?.offset || 0
          exists = !!r.data?.exists
        }
      } catch (e) {
        // 探针失败不阻塞上传，从 0 开始重传
      }

      // 目标文件已存在（或续传已完整）→ 询问用户覆盖 / 跳过 / 取消
      if (exists || offset >= file.size) {
        const action = await resolveConflict(filename)
        if (action === 'cancel') {
          transfer.setTaskStatus(taskId, 'canceled')
          return
        }
        if (action === 'skip') {
          transfer.setTaskStatus(taskId, 'done')
          refresh()
          return
        }
        // 覆盖：先清理目标文件与残留临时文件，再从头开始传
        offset = 0
        transfer.setTaskMeta(taskId, { loaded: 0 })
        try {
          await request.post('/file/upload/reset', {
            path: dir,
            sub_path: subPath,
            filename,
            file_id: fileID,
            total_size: file.size
          })
        } catch (e) {
          // 清理失败不阻塞，服务端追加上传时会在 offset=0 时重新覆盖临时文件
        }
      }

      // 同步初始 loaded（已续传字节数计入进度）
      transfer.setTaskMeta(taskId, { loaded: offset })

      // 循环上传，直到全部完成（正常情况下一次 slice 就传完；断点续传时从 offset 继续）
      while (offset < file.size) {
        const tNow = transfer.tasks.find((t) => t.id === taskId)
        if (!tNow) return
        if (tNow.paused) {
          transfer.setTaskStatus(taskId, 'paused')
          return
        }
        if (tNow.controller?.signal?.aborted) {
          transfer.setTaskStatus(taskId, 'canceled')
          return
        }

        const blob = file.slice(offset, file.size)
        const ok = await uploadSlice(blob, offset)
        if (ok === 'canceled' || ok === 'paused') {
          return
        }
        if (ok === false) {
          // 重试耗尽
          return
        }
        // 服务端返回完成或最新 offset
        if (ok.complete) {
          break
        }
        offset = ok.offset
        // 防御：offset 未前进时避免死循环
        if (offset >= file.size) break
      }

      transfer.setTaskStatus(taskId, 'done')
      refresh()
    } catch (e) {
      const tNow = transfer.tasks.find((t) => t.id === taskId)
      if (tNow && tNow.status === 'running') {
        transfer.setTaskStatus(taskId, 'error', e?.message || '上传失败')
      }
    }
  }

  transfer.registerStartFn(taskId, startFn)
  // 不立即执行：等待队列调度推进到 running
  if (typeof window !== 'undefined' && window.__lpTransferQueueTick) {
    window.__lpTransferQueueTick()
  }
}

/**
 * 上传任务队列调度：
 * - 同时只有 1 个上传任务处于 running，其余 pending
 * - 任何 running→终态后都会触发 tick 自动推进下一个 pending → running
 */
function transferQueueTick() {
  // 只检查上传任务的并发（下载不受此限）
  const uploadRunning = transfer.tasks.filter(
    (t) => t.type === 'upload' && t.status === 'running'
  ).length
  if (uploadRunning >= 1) return // 已有上传任务在跑，暂不调度下一个上传
  const next = transfer.tasks.find(
    (t) => t.type === 'upload' && t.status === 'pending' && t.startFn
  )
  if (next) {
    transfer.setTaskStatus(next.id, 'running')
  }
}

if (typeof window !== 'undefined') {
  window.__lpTransferQueueTick = transferQueueTick
}

// ============= 拖拽上传 =============
function onDragEnter(e) {
  if (e.dataTransfer) e.dataTransfer.dropEffect = 'copy'
  dragDepth++
  dragOver.value = true
}
function onDragLeave() {
  dragDepth--
  if (dragDepth <= 0) {
    dragDepth = 0
    dragOver.value = false
  }
}
async function onDrop(e) {
  e.preventDefault()
  dragOver.value = false
  dragDepth = 0
  const items = e.dataTransfer.items
  const list = []
  if (items && items.length && typeof items[0].webkitGetAsEntry === 'function') {
    const entries = []
    for (const it of items) {
      const entry = it.webkitGetAsEntry?.()
      if (entry) entries.push(entry)
    }
    for (const en of entries) {
      await walkEntry(en, '', list)
    }
  } else {
    for (const f of e.dataTransfer.files) list.push({ file: f, relativePath: f.name })
  }
  if (list.length === 0) return
  // 任意文件带子目录路径 → 视为文件夹上传（保留结构）
  const isDir = list.some((it) => {
    const p = it.relativePath || it.webkitRelativePath || it._relativePath || ''
    return p.includes('/')
  })
  uploadFiles(list, isDir)
}

// 把拖拽处理暴露给全局：TransferPanel 上的拖拽区放下文件时，会派发
// lp-transfer-drop-files 事件，这里统一处理（保证 currentPath 上下文一致）。
if (typeof window !== 'undefined') {
  window.addEventListener('lp-transfer-drop-files', async (e) => {
    const files = e.detail?.files
    if (!files || files.length === 0) return
    const list = []
    for (const f of files) {
      list.push({ file: f, relativePath: f.webkitRelativePath || f.name })
    }
    const isDir = list.some((it) => it.relativePath.includes('/'))
    uploadFiles(list, isDir)
  })
}
function walkEntry(entry, prefix, out) {
  return new Promise((resolve) => {
    if (entry.isFile) {
      entry.file((file) => {
        // 用 wrapper 对象保存相对路径，避免依赖可能被浏览器保护的 webkitRelativePath
        out.push({ file, relativePath: prefix + file.name })
        resolve()
      })
    } else if (entry.isDirectory) {
      const reader = entry.createReader()
      const readBatch = () => {
        reader.readEntries(async (entries) => {
          if (!entries || entries.length === 0) {
            resolve()
            return
          }
          for (const en of entries) {
            await walkEntry(en, prefix + entry.name + '/', out)
          }
          readBatch() // 必须循环调用直到空
        }, () => resolve())
      }
      readBatch()
    } else {
      resolve()
    }
  })
}

// ============= 下载（登录态地址，浏览器直接下载到本机） =============
// /api/file/download 需要登录态（Authorization 头或登录 cookie 均可），地址长期有效：
// 同源请求会自动携带登录 cookie，服务端以 attachment 响应，由浏览器「另存为」
// 保存到本机电脑——而不是把文件下载到服务器里。
function download(row) {
  const url = '/api/file/download?path=' + encodeURIComponent(row.path)
  const a = document.createElement('a')
  a.href = url
  a.download = row.name
  a.style.display = 'none'
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
}

// ============= 权限 =============
// 把 9 个 boolean 对象转换为 [r,w,x,r,w,x,r,w,x] 数组
function bitsFromModel() {
  const b = permBits.value
  return [b.oR, b.oW, b.oX, b.gR, b.gW, b.gX, b.xR, b.xW, b.xX]
}
// 写回勾选位（从八进制数字或 rwx 字符串）
function applyPermValueToBits(mode) {
  const raw = (mode || '').toString().trim()
  let oct
  if (/^[rwx-]{9}$/i.test(raw)) {
    oct = rwxToOctal(raw.toLowerCase())
  } else if (/^[0-7]{3,4}$/.test(raw)) {
    oct = raw.slice(-3)
  } else {
    oct = '755'
  }
  const bits = parsePermToBits(oct)
  permBits.value = {
    oR: bits[0], oW: bits[1], oX: bits[2],
    gR: bits[3], gW: bits[4], gX: bits[5],
    xR: bits[6], xW: bits[7], xX: bits[8]
  }
}
function setPermission(row) {
  permPath.value = row.path
  permValue.value = row.mode
  permOwner.value = row.user
  permGroup.value = row.group || ''
  permIsBatch.value = false
  permRecursive.value = false
  permIsDir.value = !!row.is_dir
  applyPermValueToBits(row.mode)
  loadUserGroups()
  permVisible.value = true
}
async function batchPermission() {
  if (selectedRows.value.length === 0) return
  // 仅支持统一设置一个权限/属主
  const first = selectedRows.value[0]
  permPath.value = `${selectedRows.value.length} 项（统一设置）`
  permValue.value = first.mode
  permOwner.value = first.user
  permGroup.value = first.group || ''
  permIsBatch.value = true
  permRecursive.value = false
  permIsDir.value = selectedRows.value.some((r) => r.is_dir)
  applyPermValueToBits(first.mode)
  loadUserGroups()
  permVisible.value = true
}
function resetPerm() {
  permPath.value = ''
  permValue.value = ''
  permOwner.value = ''
  permGroup.value = ''
  permIsBatch.value = false
  permRecursive.value = false
  permIsDir.value = false
  permBits.value = {
    oR: true, oW: true, oX: true,
    gR: true, gW: false, gX: true,
    xR: true, xW: false, xX: true
  }
}
function onPermValueChange(v) {
  const raw = (v || '').toString().trim()
  if (/^[rwx-]{9}$/i.test(raw) || /^[0-7]{3,4}$/.test(raw)) {
    applyPermValueToBits(raw)
  }
}
// 监听 9 个位变化：自动同步数字输入框
watch(permBits, () => {
  if (permVisible.value) {
    permValue.value = buildPermFromBits(bitsFromModel())
  }
}, { deep: true })
async function confirmPermission() {
  if (!/^[0-7]{3,4}$/.test(permValue.value)) {
    ElMessage.error('权限格式错误，应为 3-4 位数字（如 755）')
    return
  }
  permSaving.value = true
  try {
    // 批量时走 selectedRows
    const targets = permIsBatch.value
      ? selectedRows.value.map((r) => r.path)
      : [permPath.value]
    const recursive = permIsDir.value && permRecursive.value
    let ok = 0, fail = 0
    for (const p of targets) {
      try {
        const res = await request.post('/file/chmod', {
          path: p,
          mode: permValue.value,
          owner: permOwner.value,
          group: permGroup.value,
          recursive
        })
        if (res.code === 0) ok++; else fail++
      } catch { fail++ }
    }
    if (fail === 0) {
      ElMessage.success(`权限已更新：${ok} 项`)
    } else {
      ElMessage.warning(`完成：成功 ${ok}，失败 ${fail}`)
    }
    permVisible.value = false
    refresh()
  } finally {
    permSaving.value = false
  }
}

// ============= 压缩 =============
function resetCompress() {
  compressSources.value = []
  compressName.value = ''
}
async function confirmCompress() {
  if (!compressName.value.trim()) {
    ElMessage.error('请输入压缩文件名')
    return
  }
  compressDoing.value = true
  try {
    // 后端 zip 路由字段为 { paths, zip_path }；自动根据 format 决定后缀
    const fname = compressName.value.trim().replace(/\.(zip|tar\.gz|tgz)$/i, '')
    const ext = compressFormat.value === 'tar.gz' ? 'tar.gz' : 'zip'
    const zipPath = currentPath.value.replace(/\/+$/, '') + '/' + fname + '.' + ext
    const res = await request.post('/file/zip', {
      paths: compressSources.value.map((s) => s.path),
      zip_path: zipPath
    })
    if (res.code !== 0) {
      ElMessage.error(res.msg || '压缩失败')
      return
    }
    ElMessage.success('压缩完成')
    compressVisible.value = false
    refresh()
  } catch (e) {
    ElMessage.error('压缩失败: ' + (e?.message || e))
  } finally {
    compressDoing.value = false
  }
}

// ============= 搜索 =============
let searchTimer = null
function onSearch() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(doSearch, 300)
}
async function doSearch() {
  const q = search.value.trim()
  if (!q) {
    searchMode.value = false
    searchResults.value = []
    return
  }
  searchMode.value = true
  searchLoading.value = true
  try {
    const res = await request.post('/file/search', { path: currentPath.value, keyword: q, max: 200 })
    if (res.code !== 0) {
      ElMessage.error(res.msg || '搜索失败')
      return
    }
    searchResults.value = res.data || []
    searchModeInZip.value = !!res.in_zip
  } catch (e) {
    ElMessage.error('搜索失败: ' + (e?.message || e))
  } finally {
    searchLoading.value = false
  }
}
function onSearchRowDblClick(row) {
  window.getSelection()?.removeAllRanges()
  if (row.is_dir) {
    cd(row.path)
    searchMode.value = false
    search.value = ''
  } else if (!row.is_dir && row.dir) {
    cd(row.dir)
    searchMode.value = false
    search.value = ''
  }
}

// ============= Tab 切换 / 关闭 / 新增 =============
function switchTab(id) {
  if (id === tabs.activeId) return
  const t = tabs.tabs.find((x) => x.id === id)
  if (!t) return
  tabs.activeId = id
  tabs.persist()
  if (t.path !== currentPath.value) {
    // 切到目标 tab 后，目标 tab 自己的 history 在 store 中已持久化，
    // syncHistory 会把 historyIdx 移到 t.path 处，前进/后退在该 tab 内独立工作
    listDir(t.path, false)
  }
  // 切 tab 用 replaceState：把浏览器历史顶条目替换为新 tab 的当前路径
  // 这样浏览器 back 不会回到刚被切换掉的 tab 的历史
  replaceBrowserHistory(t.path)
  // 不再写 URL query，避免地址栏污染（路径信息都在 fileTabs store 中持久化）
}
function closeTabHandler(id) {
  if (tabs.tabs.length <= 1) {
    ElMessage.warning('至少保留一个标签')
    return
  }
  const wasActive = id === tabs.activeId
  tabs.closeTab(id)
  if (wasActive) {
    const t = tabs.tabs.find((x) => x.id === tabs.activeId)
    if (t && t.path !== currentPath.value) {
      listDir(t.path, false)
    }
    if (t) replaceBrowserHistory(t.path)
  }
}
function addTabPrompt() {
  // 直接打开根目录作为新标签：避免弹窗要求用户输入路径
  const root = '/'
  const found = tabs.tabs.find((t) => t.path === root)
  if (found) {
    switchTab(found.id)
    return
  }
  tabs.addTab(root)
  listDir(root, false)
  replaceBrowserHistory(root)
}

// ============= 跨 tab 剪贴板同步 =============
function onStorage(e) {
  if (e.key === 'panel_file_clipboard') {
    clip.syncFromStorage()
  }
}

// ============= 生命周期 =============
onMounted(() => {
  // 1) URL ?path=xxx 优先（外部链接携带）
  // 2) 否则从 nav store 取一次性路径（Website 卡片"打开"按钮进来）
  // 3) 否则用 fileTabs store 持久化的激活标签路径
  const qp = router.currentRoute.value.query.path
  let initPath = qp ? String(qp) : ''
  if (!initPath) {
    const navPath = nav.consumeFilePath()
    if (navPath) initPath = navPath
  }
  if (initPath) {
    const found = tabs.tabs.find((t) => t.path === initPath)
    if (found) {
      tabs.activeId = found.id
      tabs.persist()
    } else {
      tabs.addTab(initPath)
    }
  }
  const active = tabs.tabs.find((t) => t.id === tabs.activeId) || tabs.tabs[0]
  const startPath = (active && active.path) || '/root'
  currentPath.value = startPath
  // 首次挂载：用 replaceState 写入起点（不 push），避免污染浏览器历史
  // pushHistory=false：不重复入栈，tab.history 已在 addTab 时初始化
  replaceBrowserHistory(startPath)
  listDir(startPath, false)
  window.addEventListener('storage', onStorage)
  document.addEventListener('click', onDocumentClick)
  document.addEventListener('keydown', onKeyDown)
  window.addEventListener('popstate', onPopState) // 浏览器前进/后退 → 返回上一层目录
  // 后台预加载系统用户/组列表（首次打开权限弹窗时会更快）
  loadUserGroups()
})
onBeforeUnmount(() => {
  window.removeEventListener('storage', onStorage)
  document.removeEventListener('click', onDocumentClick)
  document.removeEventListener('keydown', onKeyDown)
  window.removeEventListener('popstate', onPopState)
})

// 监听 route 变化（外部跳转到本页面携带 ?path=xxx）
watch(() => router.currentRoute.value.query.path, (p) => {
  if (!p) return
  const path = String(p)
  // 同步切到/添加标签
  const found = tabs.tabs.find((t) => t.path === path)
  if (found) {
    if (tabs.activeId !== found.id) tabs.activeId = found.id
  } else {
    tabs.addTab(path)
  }
  if (path !== currentPath.value) listDir(path, true)
})
// 当前路径变化时同步写回激活标签
watch(currentPath, (p) => {
  if (!p) return
  tabs.updateTabPath(tabs.activeId, p)
})
</script>

<style scoped>
/* v20260825-1610 */
.file-manager {
  position: relative;
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  background: #f5f7fa;
  overflow: visible;
}

/* 表格内操作按钮行：靠右对齐；桌面端单行（nowrap），<800px 视口允许折成 2 行 */
.row-actions {
  display: flex;
  justify-content: flex-end;  /* 列内靠右对齐 */
  align-items: center;
  flex-wrap: nowrap;          /* 桌面端单行 */
  white-space: nowrap;
  font-size: 12px;
  gap: 0;
  margin: 0;
  padding: 0;
  line-height: 1;
  width: 100%;
}
@media (max-width: 800px) {
  .row-actions {
    flex-wrap: wrap;          /* 窄屏折成 2 行 */
    row-gap: 2px;
  }
}
/* 移动端行内操作按钮：紧凑小图标按钮，隐藏文字 */
@media (max-width: 767px) {
  .row-actions { flex-wrap: wrap; row-gap: 2px; column-gap: 0; }
  .row-actions .el-button {
    font-size: 11px;
    padding: 1px 4px;
    min-width: 0;
  }
  /* 「打开/压缩/解压/下载」隐藏文字，只留紧凑按钮 */
  .row-actions .el-button:not(.row-more-btn) {
    padding: 1px 5px;
  }
  .row-actions .el-dropdown > .el-button.row-more-btn { padding: 1px 4px; }
  /* 名称列只显示文件名首段，省空间 */
  .name-cell { min-width: 0; }
  .name-cell .name-text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 100px;
  }
  /* 移动端表格列压缩：通过强制设置 el-table colgroup 列宽 */
  .el-table colgroup col:nth-child(3),
  .el-table colgroup col:nth-child(4),
  .el-table colgroup col:nth-child(5) { width: 0 !important; }
  /* 隐藏第 3-5 列表头和单元格（类型/大小/权限） */
  .el-table .el-table__cell:nth-child(3),
  .el-table .el-table__cell:nth-child(4),
  .el-table .el-table__cell:nth-child(5),
  .el-table .el-table__header .cell:nth-child(3),
  .el-table .el-table__header .cell:nth-child(4),
  .el-table .el-table__header .cell:nth-child(5) { display: none !important; }
  /* 名称列拉到更宽 */
  .el-table colgroup col:nth-child(2) { width: auto !important; }
  /* 强制类型列宽度为 0，避免 el-table 仍然给它分配空间 */
  .el-table .el-table__column.is-leaf:nth-child(3),
  .el-table .el-table__column.is-leaf:nth-child(4),
  .el-table .el-table__column.is-leaf:nth-child(5) { width: 0 !important; }
  /* 调整表头列对应隐藏 */
  .el-table .el-table__header-wrapper colgroup col:nth-child(3),
  .el-table .el-table__header-wrapper colgroup col:nth-child(4),
  .el-table .el-table__header-wrapper colgroup col:nth-child(5) { width: 0 !important; }
  /* 隐藏类型表头 */
  .el-table__header-wrapper .el-table__cell:nth-child(3),
  .el-table__header-wrapper .el-table__cell:nth-child(4),
  .el-table__header-wrapper .el-table__cell:nth-child(5) { display: none !important; }
}
/* 桌面宽屏：一行排开（默认 wrap 已够，无需额外约束） */
/* 统一按钮字号/内边距；显式归零所有外边距，避免 el-button + el-button 自带 10px 间距 */
.row-actions > * {
  margin: 0 !important;
  padding: 0;
}
.row-actions .el-button {
  min-height: auto;
  font-size: 12px;
  padding: 2px 4px;
  background: transparent;
  line-height: 1.2;
}
.row-actions .el-dropdown {
  font-size: 12px;
  line-height: 1;
}
/* 让 el-dropdown 的外层 div 不再撑出额外内边距 */
.row-actions .el-dropdown > .el-button {
  padding: 2px 4px;
}
.row-actions .el-dropdown__popper {
  font-size: 13px;
}
/* 「更多」箭头：内置一个小三角形，去掉 el-icon 占位，自带紧凑 */
.row-actions .row-more-btn {
  display: inline-flex;
  align-items: center;
  gap: 2px;
}
.row-actions .el-icon-more-arrow {
  width: 0;
  height: 0;
  border-left: 4px solid transparent;
  border-right: 4px solid transparent;
  border-top: 4px solid currentColor;
  margin-left: 2px;
  font-style: normal;
  display: inline-block;
  opacity: .8;
}

/* 文件列表右键菜单 */
.context-menu {
  position: fixed;
  z-index: 2000;
  min-width: 140px;
  padding: 4px 0;
  background: #fff;
  border: 1px solid #e4e7ed;
  border-radius: 4px;
  box-shadow: 0 2px 12px 0 rgba(0, 0, 0, 0.1);
  user-select: none;
}
.context-menu-item {
  display: flex;
  align-items: center;
  padding: 8px 16px;
  font-size: 13px;
  color: #606266;
  cursor: pointer;
  transition: background-color 0.15s;
}
.context-menu-item:hover {
  background-color: #f5f7fa;
  color: #409eff;
}
.context-menu-item.divided {
  border-top: 1px solid #ebeef5;
}
.context-menu-icon {
  margin-right: 8px;
  font-size: 14px;
}

/* 多标签栏 */
.tab-bar {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 6px 10px 0 10px;
  background: #fff;
  border-bottom: 1px solid #ebeef5;
  flex-wrap: nowrap;
}

.tab-item {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-width: 80px;
  max-width: 200px;
  padding: 6px 10px;
  border: 1px solid #dcdfe6;
  border-bottom: none;
  border-radius: 6px 6px 0 0;
  background: #fafbfc;
  color: #606266;
  cursor: pointer;
  font-size: 13px;
  user-select: none;
  position: relative;
  top: 1px;
  transition: background 0.12s, color 0.12s;
}
.tab-item:hover {
  background: #ecf5ff;
  color: #409eff;
}
.tab-item.active {
  background: #fff;
  color: #409eff;
  border-color: #409eff;
  font-weight: 600;
  z-index: 1;
}
.tab-item .tab-icon {
  font-size: 13px;
  flex-shrink: 0;
}
.tab-item .tab-name {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  flex: 1;
}
.tab-item .tab-close {
  margin-left: 2px;
  width: 16px;
  height: 16px;
  line-height: 14px;
  text-align: center;
  border-radius: 50%;
  font-size: 12px;
  color: #909399;
  flex-shrink: 0;
  display: inline-block;
}
.tab-item .tab-close:hover {
  background: #f56c6c;
  color: #fff;
}
.tab-item.closable:hover .tab-close {
  /* 只有多于一个标签才显示关闭 */
  display: inline-block;
}
/* 单标签时不显示关闭按钮（由模板 v-if 控制） */


.tab-add {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: 4px;
  cursor: pointer;
  color: #606266;
  font-size: 14px;
  flex-shrink: 0;
}
.tab-add:hover {
  background: #ecf5ff;
  color: #409eff;
}


.toolbar {
  display: flex;
  flex-wrap: wrap;          /* 永远允许换行，让 flex 子项在小屏自然换行 */
  align-items: center;
  justify-content: flex-start;
  padding: 8px 14px;
  background: #fff;
  border-bottom: 1px solid #ebeef5;
  /* 组间 16px 间隙，组内 0（组内由 tb-group 自身 gap: 0 控制） */
  gap: 8px 16px;
}

/* 把 4 个分组的"视觉"顺序交给 order 控制；组内按钮紧贴，组间靠 toolbar 的 gap 分隔 */
.tb-group {
  display: flex;
  align-items: center;
  gap: 0;
  min-width: 0;
}
/* 终端按钮的 inline SVG 图标：比 Element Plus 内置图标略大、更显眼 */
.terminal-icon {
  width: 16px;
  height: 16px;
  vertical-align: -2px;
}

/* ========= 桌面端默认 (≥1200px) ========= */
/* 3 个组横向排：
   组 1 (导航+路径) 优先被挤压；组 2 (搜索) 默认 220px，路径栏压不动后再被挤压；
   组 3 (操作) 固定宽度不参与挤压 */
.tb-nav    { order: 1; flex: 1 2 auto;  min-width: 120px; flex-wrap: nowrap; }
.tb-nav .el-button-group { flex-shrink: 0; flex-wrap: nowrap; border-top-right-radius: 0 !important; border-bottom-right-radius: 0 !important; overflow: hidden; }
.tb-nav .el-button-group > .el-button:last-child { border-top-right-radius: 0 !important; border-bottom-right-radius: 0 !important; }
.tb-nav .path-bar { flex: 1 1 auto; min-width: 0; }
.tb-search { order: 2; flex: 0 0 auto; }  /* 不伸缩，宽度由 input+button 内容决定 */
.tb-search-input { width: 220px !important; }  /* input 默认 220px */
.tb-actions { order: 3; flex: 0 0 auto; display: flex; align-items: center; gap: 8px; }
.tb-actions .el-button,
.tb-actions .el-dropdown {
  flex: 0 0 auto;
  margin: 0 !important;  /* 覆盖 el-dropdown 默认的 margin-left: 12px */
}

/* 工具栏作为 container，根据自身宽度触发断点 */
.toolbar { container-type: inline-size; container-name: fm-toolbar; }

/* ========= 平板 (768–1199px) =========
   分 2 行：行1=导航+路径；行2=搜索+搜索按钮+4 个操作按钮（同一行） */
@media (max-width: 1199px) {
  .tb-nav { width: 100%; flex: none; min-width: 0; }
  .tb-nav .path-bar { flex: 1 1 auto; min-width: 0; }
  .tb-search { flex: 1 1 auto; min-width: 0; }
  .tb-search-input { flex: 1 1 auto; min-width: 0; width: auto; }
  .tb-actions { flex: 0 0 auto; flex-wrap: nowrap; gap: 4px; margin-left: 8px; }
  .tb-actions .el-button,
  .tb-actions .el-dropdown { flex: 0 0 auto; }
  .tb-actions .el-button,
  .tb-actions .el-dropdown > .el-button { padding-left: 6px; padding-right: 6px; }
}

/* ========= 移动端 (<768px) =========
   分 3 行：行1=导航+路径；行2=搜索框+搜索按钮（单独）；行3=4 个操作按钮占满 */
@media (max-width: 767px) {
  .tb-nav { width: 100%; flex: none; min-width: 0; }
  .tb-nav .path-bar { flex: 1 1 auto; min-width: 0; }
  .tb-search { width: 100%; flex: none; }
  .tb-search-input { flex: 1 1 auto; min-width: 0; width: auto; }
  .tb-actions { width: 100%; flex: none; flex-wrap: nowrap; gap: 4px; margin-left: 0; }
  .tb-actions .el-button,
  .tb-actions .el-dropdown { flex: 1 1 0; min-width: 0; }
}

/* 搜索框和搜索按钮无缝拼接：去右圆角 + 去左圆角 + 负 margin 重叠 1px 消除边框缝 */
.tb-search .el-input {
  border-top-right-radius: 0 !important;
  border-bottom-right-radius: 0 !important;
  overflow: hidden;
}
.tb-search .el-input__wrapper {
  border-top-right-radius: 0 !important;
  border-bottom-right-radius: 0 !important;
  box-shadow: 0 0 0 1px var(--el-input-border-color, var(--el-border-color)) inset !important;
}
.tb-search-btn {
  /* margin-left: -1px 吃掉搜索框右边 1px 边框，让两者完全紧贴无缝 */
  margin: 0 0 0 -1px;
  border-top-left-radius: 0 !important;
  border-bottom-left-radius: 0 !important;
  box-shadow: none !important;  /* 去掉 el-button 默认内阴影，避免视觉缝 */
}

.toolbar-left, .toolbar-right {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.path-crumb {
  margin-left: 0;
  font-size: 14px;
}

/* 面板风格地址栏：根目录 + 段按钮 + › 分隔；整条可点击进入编辑态 */
.path-bar {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  flex: 1 1 auto;
  min-width: 0;
  width: 100%;
  /* 严格对齐 el-button default 高度（32px），box-sizing 包含 border+padding */
  height: 32px;
  box-sizing: border-box;
  padding: 0 8px;
  border: 1px solid var(--el-border-color-lighter, #dcdfe6);
  /* 紧贴前面的 el-button-group：去掉左边圆角；右边圆角保留（组间有空隙） */
  border-radius: 0 4px 4px 0;
  background: var(--el-fill-color-blank, #ffffff);
  transition: border-color .15s, box-shadow .15s;
  /* 长路径支持横向滚动，但视觉上不显示滚动条（用户用 JS 自动滚到最右 + 触屏滑动） */
  overflow-x: auto;
  overflow-y: hidden;
  -webkit-overflow-scrolling: touch;
  scrollbar-width: none;            /* Firefox 隐藏滚动条 */
  cursor: pointer;          /* 整条可点击 —— 点空白处直接进编辑 */
}
/* Chrome / Safari：隐藏滚动条但保留滚动能力 */
.path-bar::-webkit-scrollbar { display: none; width: 0; height: 0; }
.path-bar:hover { border-color: var(--el-color-primary); }
.path-bar.is-editing {
  cursor: text;
  border-color: var(--el-color-primary);
  box-shadow: 0 0 0 2px rgba(64,158,255,.15);
}
.path-bar .path-crumb {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  flex-wrap: nowrap;
  /* 长路径时由 .path-bar 的 overflow-x 滚动，这里不截断 */
  overflow: visible;
  white-space: nowrap;
  text-overflow: clip;
}
.path-bar .crumb {
  background: transparent;
  border: 0;
  padding: 2px 6px;
  margin: 0;
  font: inherit;
  color: var(--el-text-color-regular, #606266);
  border-radius: 3px;
  cursor: pointer;
  transition: background-color .12s, color .12s;
  display: inline-flex;
  align-items: center;
  white-space: nowrap;
}
.path-bar .crumb:hover {
  background: var(--el-color-primary-light-9, #ecf5ff);
  color: var(--el-color-primary);
}
.path-bar .crumb.is-last {
  font-weight: 600;
  cursor: default;
  color: var(--el-text-color-primary, #303133);
  background: transparent;
}
.path-bar .crumb.is-last:hover { color: var(--el-text-color-primary, #303133); }
.path-bar .crumb-sep {
  color: var(--el-text-color-secondary, #909399);
  font-size: 14px;
  user-select: none;
  margin: 0 1px;
}
.path-bar .path-input {
  flex: 1;
  min-width: 0;
  border: 0;
  outline: none;
  background: transparent;
  padding: 2px 4px;
  font: inherit;
  color: var(--el-text-color-primary, #303133);
  cursor: text;
}

/* 剪贴板粘性状态条 */
.clipboard-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 8px 16px;
  font-size: 14px;
  border-bottom: 1px solid #ebeef5;
  position: sticky;
  top: 0;
  z-index: 5;
}
.clipboard-bar.is-copy {
  background: #ecf5ff;
  color: #409eff;
  border-color: #b3d8ff;
}
.clipboard-bar.is-cut {
  background: #fdf6ec;
  color: #e6a23c;
  border-color: #f5dab1;
}
.cb-left {
  display: flex;
  align-items: center;
  gap: 8px;
}
.cb-title {
  font-weight: 600;
}
.cb-source {
  color: #909399;
  font-weight: 400;
  margin-left: 6px;
  font-size: 12px;
}
.cb-right {
  display: flex;
  gap: 6px;
}

/* 名称/操作列表头：选中时切换为批量操作行，避免上方 batch-bar 出现/消失导致页面抖动 */
.nh-wrap {
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 32px;
  flex-wrap: wrap;
}
.nh-label {
  color: var(--el-text-color-regular);
}

/* 批量操作条（已弃用：内容搬到名称/操作列表头 #header slot） */
.batch-bar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 6px 14px;
  background: #fffbe6;
  border-bottom: 1px solid #fde68a;
  font-size: 13px;
}
/* 选中后的批量操作栏：absolute 覆盖表头整行（不再替换单列导致错位） */
.bulk-bar-overlay {
  position: absolute;
  top: 0;
  left: 0;
  right: 0;
  /* 高度与默认表头完全对齐：未选中 → 选中 切换时表格内容区不抖动 */
  height: var(--el-table-header-height, 40px);
  z-index: 3;
  background: var(--el-fill-color-light, #f5f7fa);
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 12px;
  border-bottom: 1px solid var(--el-table-border-color, #ebeef5);
  color: var(--el-text-color-regular);
  font-size: 14px;
  pointer-events: auto;
  box-sizing: border-box;
  overflow: hidden; /* 内容超出时隐藏，避免撑高 */
}
.bulk-bar-overlay .el-checkbox {
  margin-right: 2px;
  display: flex;
  align-items: center;
}
.bulk-bar-overlay .el-button {
  font-size: 12px;
  margin-left: 0;
  /* 紧凑按钮：缩小垂直 padding，让批量栏整体高度与默认表头一致 */
  padding-top: 0;
  padding-bottom: 0;
  height: 24px;
  line-height: 24px;
}
.bulk-bar-overlay .bb-left {
  display: flex;
  align-items: center;
  gap: 10px;
}
.bulk-bar-overlay .bb-right {
  display: flex;
  align-items: center;
  gap: 6px;
}
.bulk-fade-enter-active,
.bulk-fade-leave-active {
  transition: opacity 0.15s ease;
}
.bulk-fade-enter-from,
.bulk-fade-leave-to {
  opacity: 0;
}
.bb-left {
  display: flex;
  align-items: center;
  gap: 10px;
}
.bb-text {
  color: #606266;
}
.bb-right {
  display: flex;
  gap: 4px;
}

/* 移动端批量操作条：左右两列紧凑布局；左列竖排文字操作，右列竖排图标按钮 */
@media (max-width: 767px) {
  .bulk-bar-overlay {
    flex-direction: row;
    align-items: center;
    padding: 4px 6px;
    font-size: 11px;
    gap: 6px;
    min-height: 40px;
  }
  .bb-left {
    display: flex;
    flex-direction: row;
    align-items: center;
    gap: 6px;
    flex: 1 1 auto;
    min-width: 0;
    padding-bottom: 0;
    border-bottom: none;
    flex-wrap: wrap;
  }
  .bb-left .bb-text { white-space: nowrap; font-size: 11px; }
  .bb-left .el-checkbox { transform: scale(0.85); flex: none; margin: 0; }
  .bb-left .el-button { padding: 2px 6px; font-size: 11px; height: 22px; line-height: 22px; margin: 0; }
  /* 右侧 5 个按钮：网格 5 列一行，保留图标 + 文字 */
  .bb-right {
    display: grid;
    grid-template-columns: repeat(5, 1fr);
    flex: 1 1 auto;
    gap: 2px;
    min-width: 0;
    align-items: stretch;
  }
  .bb-right .el-button {
    flex: none;
    width: auto;
    min-width: 0;
    padding: 0 8px;
    height: 24px;
    margin: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 2px;
    font-size: 11px;
  }
  .bb-right .el-button .el-icon { font-size: 13px; }
  /* 文字标签正常显示 */
  .bb-right .bb-label { display: inline; white-space: nowrap; }
  /* 手机端空间紧张：左栏只保留"全选复选框 + 已选 N 项"，反选/取消选择都隐藏 */
  .bb-left .bb-btn-inverse { display: none !important; }
  .bb-left .bb-btn-cancel-select { display: none !important; }
}

/* 搜索提示 */

/* 搜索提示 */
.search-tip {
  padding: 6px 14px;
  background: #f4f4f5;
  color: #909399;
  font-size: 12px;
  border-bottom: 1px solid #ebeef5;
}

/* 内容区 */
.content-area {
  flex: 1;
  min-height: 0;
  position: relative;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: #fff;
}
.file-grid, .search-results {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  background: #fff;
  overflow: auto;
}
.file-grid {
  position: relative;
}
.file-grid > .el-table,
.search-results > .el-table {
  flex: 1;
}
/* Windows 风格框选矩形：半透明填充 + 蓝色实线边框 */
.drag-sel-rect {
  position: absolute;
  z-index: 999;
  background: rgba(64, 158, 255, 0.12);
  border: 1px solid rgba(64, 158, 255, 0.7);
  pointer-events: none;
}
/* el-table 不写 height：行少自然贴内容，行多交给 .content 外层滚动条 */
.file-empty :deep(.el-empty__description) {
  padding: 8px 0;
}
.empty-drop-hint {
  margin-top: 8px;
  color: #909399;
  font-size: 13px;
}

.name-cell {
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  min-width: 0;
  width: 100%;
}
.name-text {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  min-width: 0;
  flex: 1 1 auto;
}
.result-row {
  display: flex;
  align-items: center;
  gap: 6px;
}
.rename-input {
  max-width: 280px;
}
.mode-cell {
  font-family: 'Consolas', 'Monaco', monospace;
  cursor: pointer;
  color: #409eff;
}
.mode-cell:hover {
  text-decoration: underline;
}

/* 文件夹大小：未计算时显示为可点击链接 */
.dir-size {
  color: #606266;
  font-family: 'Consolas', 'Monaco', monospace;
  font-size: 12px;
}
.dir-size-btn {
  padding: 0 4px;
  margin: 0;
  font-size: 12px;
  color: #409eff;
}
.dir-size-btn:disabled,
.dir-size-btn.is-loading {
  color: #909399;
}

.result-name {
  font-weight: 500;
  margin-right: 8px;
}
.result-path {
  color: #909399;
  font-size: 12px;
}

/* 拖拽遮罩 */
.drag-mask {
  position: absolute;
  inset: 0;
  background: rgba(64, 158, 255, 0.12);
  border: 2px dashed #409eff;
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 999;
  pointer-events: none;
}
.drag-inner {
  text-align: center;
  color: #409eff;
}
.drag-tip {
  margin-top: 8px;
  font-size: 16px;
}

/* 上传冲突对话框 */
.conflict-body {
  display: flex;
  align-items: flex-start;
  gap: 10px;
}
.conflict-icon {
  color: #e6a23c;
  flex-shrink: 0;
  margin-top: 2px;
}
.conflict-msg {
  font-size: 14px;
  line-height: 1.6;
  color: #606266;
}
.conflict-msg b {
  color: #303133;
  word-break: break-all;
}

/* 分页 */
.pagination {
  padding: 8px 14px;
  display: flex;
  justify-content: flex-end;
  background: #fff;
  border-top: 1px solid #ebeef5;
}

/* 权限对话框 - 完全复刻设置页样式 */
.perm-form {
  padding: 0 4px;
}
.perm-path {
  font-family: 'Consolas', 'Monaco', monospace;
  color: #606266;
  word-break: break-all;
}
/* 权限下拉宽度撑满表单 */
.perm-select { width: 100%; }
.perm-select .el-select__wrapper {
  min-height: 36px;
}
.perm-card-row {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 10px;
  width: 100%;
  padding: 0 0 4px 0;
}
.perm-card {
  background: #fff;
  border: 1px solid #ebeef5;
  border-radius: 6px;
  padding: 10px 12px;
  text-align: left;
}
.perm-card-title {
  font-size: 13px;
  color: #606266;
  margin-bottom: 6px;
  font-weight: 600;
}
.perm-card :deep(.el-checkbox) {
  display: flex;
  align-items: center;
  margin-right: 0;
  height: 22px;
  margin-bottom: 2px;
  white-space: nowrap;
}
.perm-card :deep(.el-checkbox__label) {
  padding-left: 6px;
}
.perm-owner-row {
  width: 100%;
}
.perm-owner-row .el-col {
  max-width: 50%;
}

/* 压缩 */
.compress-sources {
  max-height: 120px;
  overflow: auto;
  border: 1px solid #ebeef5;
  border-radius: 4px;
  padding: 6px 8px;
}

/* 预览 */
.preview-image-wrap {
  text-align: center;
}
.preview-image {
  max-width: 100%;
  max-height: 70vh;
}
.preview-media {
  max-width: 100%;
  max-height: 70vh;
}
.preview-audio {
  width: 400px;
}

/* 表格覆盖 */
:deep(.el-table) {
  --el-table-header-bg-color: #fafbfc;
}
:deep(.el-table__row.is-renaming) {
  background: #f0f9ff !important;
}
:deep(.el-table__row) {
  cursor: default;
}
/* 鼠标按下拖动批量选择：阻止浏览器默认文本选中（重命名输入框除外） */
.file-grid :deep(.el-table__row),
.search-results :deep(.el-table__row) {
  -webkit-user-select: none;
  user-select: none;
}
.file-grid :deep(.el-table__row .el-input__inner),
.search-results :deep(.el-table__row .el-input__inner) {
  -webkit-user-select: text;
  user-select: text;
}

/* 代码编辑器弹窗：默认居中弹窗，可切换全屏 */
:deep(.file-editor-dialog) {
  margin-top: 5vh !important;
  height: 85vh;
  display: flex;
  flex-direction: column;
  background: transparent;
  box-shadow: none;
  border: none;
}
:deep(.file-editor-dialog .el-dialog__header) {
  display: none;
}
:deep(.file-editor-dialog .el-dialog__body) {
  flex: 1;
  overflow: hidden;
  padding: 0;
  background: #1e1e1e;
  border-radius: 6px;
  box-shadow: 0 8px 32px rgba(0, 0, 0, 0.5);
}
:deep(.file-editor-dialog.is-fullscreen) {
  margin: 0 !important;
  width: 100% !important;
  height: 100%;
  border-radius: 0;
}
:deep(.file-editor-dialog.is-fullscreen .el-dialog__body) {
  border-radius: 0;
}
:deep(.file-editor-dialog .fe) {
  height: 100%;
}
/* 移动端：编辑器弹窗占满全屏 */
@media (max-width: 767px) {
  :deep(.file-editor-dialog) {
    width: 100% !important;
    height: 100%;
    margin: 0 !important;
  }
  :deep(.file-editor-dialog .el-dialog__body) {
    border-radius: 0;
  }
}
</style>
