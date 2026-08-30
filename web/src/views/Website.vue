<template>
  <div class="website">
    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>网站管理</span>
          <div class="card-header-actions">
            <el-button
              v-if="(activeTab === 'php' || activeTab === 'python' || activeTab === 'node' || activeTab === 'go') && currentRuntimeEnv?.installed"
              type="success"
              @click="openEnvManager"
            >
              <el-icon><Tools /></el-icon>&nbsp;{{ typeMeta[activeTab]?.label }} 环境管理
            </el-button>
            <el-button type="primary" :disabled="!nginxInstalled" @click="openCreate">
              <el-icon><Plus /></el-icon>&nbsp;创建网站
            </el-button>
            <el-button :disabled="!nginxInstalled" @click="openDefaultPages">
              <el-icon><Files /></el-icon>&nbsp;默认页面
            </el-button>
          </div>
        </div>
      </template>

      <!-- 类型 Tab -->
      <el-tabs v-model="activeTab" class="site-tabs" @tab-change="onTabChange">
        <el-tab-pane
          v-for="t in tabs"
          :key="t.key"
          :name="t.key"
        >
          <template #label>
            <span :class="{ 'tab-count': true }">
              {{ t.label }}
              <el-badge v-if="t.count > 0" :value="t.count" :type="t.badgeType" class="tab-badge" />
            </span>
          </template>
        </el-tab-pane>
      </el-tabs>

      <!-- Web 服务器安装/卸载中遮罩 -->
      <div v-if="installingNginx" class="env-missing">
        <div class="env-missing-inner">
          <el-icon :size="48" color="#409eff"><Warning /></el-icon>
          <h3 v-if="pendingActionMap.nginx === 'uninstall'">正在卸载 Web 服务器...</h3>
          <h3 v-else>正在安装 Web 服务器...</h3>
          <p>{{ pendingActionMap.nginx === 'uninstall' ? '卸载进行中，请稍候...' : '安装进行中，请稍候...' }}</p>
        </div>
      </div>

      <!-- Web 服务器未安装遮罩提示（nginx / apache） -->
      <div v-else-if="!nginxInstalled" class="env-missing">
        <div class="env-missing-inner">
          <el-icon :size="48" color="#e6a23c"><Warning /></el-icon>
          <h3>未检测到 Web 服务器环境</h3>
          <p>{{ nginxEnv?.remarks || '请先安装 Nginx 或 Apache 后管理网站' }}</p>
          <el-button type="primary" size="large" :loading="installingNginx" @click="installNginx">
            <el-icon><Download /></el-icon> 一键安装 {{ nginxEnv?.name || 'Web 服务器' }}
          </el-button>
          <p v-if="installingNginx" class="installing-hint">安装进行中，请稍候...</p>
        </div>
      </div>

      <!-- 运行时环境未安装遮罩提示（PHP / Python / Go / Node） -->
      <div v-else-if="runtimeMissing" class="env-missing" style="margin-top: 12px;">
        <div class="env-missing-inner">
          <el-icon :size="48" color="#e6a23c"><Warning /></el-icon>
          <h3>未检测到 {{ typeMeta[activeTab]?.label }} 环境</h3>
          <p>{{ runtimeMissing.remarks || `未检测到 ${typeMeta[activeTab]?.label} 命令，请先在应用商店安装 ${typeMeta[activeTab]?.label}` }}</p>
          <!-- 运行时多版本选择（PHP / Python / Go / Node 统一） -->
          <div v-if="runtimeMissing._versions?.length" class="version-select-row">
            <span>选择版本（可多选一起装）：</span>
            <el-select
              v-model="selectedRuntimeVersion"
              size="large"
              multiple
              clearable
              style="width: 360px; max-width: 100%;"
              :max-collapse-tags="999"
              :collapse-tags="false"
            >
              <el-option v-for="v in runtimeMissing._versions" :key="v.key" :label="v.name" :value="v.key" />
            </el-select>
          </div>
          <el-button
            type="primary"
            size="large"
            :disabled="runtimeInstallKey.length === 0"
            :loading="isInstallingCurrentRuntime"
            @click="quickInstall(runtimeInstallKey, runtimeMissing.name || typeMeta[activeTab]?.label)"
          >
            <el-icon><Download /></el-icon>
            {{ runtimeInstallKey.length === 0 ? '请先选择要安装的版本' : `一键安装${runtimeInstallKey.length > 1 ? ` (${runtimeInstallKey.length})` : ''}` }}
          </el-button>
          <p v-if="isInstallingCurrentRuntime" class="installing-hint">
            正在安装 {{ currentInstallingLabel }}...
          </p>
        </div>
      </div>

      <Skeleton v-else-if="loading" type="table" :rows="8" :columns="[{width:'60px'},{width:'160px'},{flex:1},{width:'140px'},{width:'140px'},{width:'200px'}]" />
      <el-table v-else :data="filteredSites" empty-text="该分类下暂无网站，点击右上角创建">
        <el-table-column prop="id" label="ID" width="60" />
        <el-table-column label="名称" width="180">
          <template #default="{ row }">
            <div class="remark-text" :class="{ empty: !row.name }" @click="editSiteName(row)">
              <span>{{ row.name }}</span>
              <el-icon class="edit-icon"><Edit /></el-icon>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="域名 / 地址" min-width="220">
          <template #default="{ row }">
            <template v-if="siteAllDomains(row).length">
              <div
                class="domain-cell"
                :class="{ 'has-more': siteAllDomains(row).length > 1 }"
                @mouseenter="openDomainList($event, row)"
                @mouseleave="closeDomainList"
              >
                <a :href="visitHref(row)" target="_blank" class="domain-link">
                  {{ siteAllDomains(row)[0] }}
                </a>
                <el-button class="copy-btn" size="small" text :title="`复制域名 ${siteAllDomains(row)[0]}`" @click="copyDomain(siteAllDomains(row)[0])">
                  <el-icon><CopyDocument /></el-icon>
                </el-button>
                <DomainQrcode :domain="siteAllDomains(row)[0]" :port="row.port" :is-https="!!row.ssl_enabled" />
              </div>
            </template>
            <span v-else>—</span>
          </template>
        </el-table-column>
        <el-table-column label="端口" width="80">
          <template #default="{ row }">{{ row.port }}</template>
        </el-table-column>
        <el-table-column label="类型" width="110">
          <template #default="{ row }">
            <el-tag size="small" :type="typeMeta[row.type]?.tag || 'info'">
              {{ typeMeta[row.type]?.label || row.type }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="运行版本" width="120" show-overflow-tooltip>
          <template #default="{ row }">
            <span v-if="row.runtime_version">{{ row.runtime_version }}</span>
            <span v-else>—</span>
          </template>
        </el-table-column>
        <el-table-column label="网站目录" min-width="200" show-overflow-tooltip>
          <template #default="{ row }">
            <el-link v-if="siteDir(row)" type="primary" @click="openFiles(siteDir(row))">{{ siteDir(row) }}</el-link>
            <span v-else>—</span>
          </template>
        </el-table-column>
        <el-table-column label="SSL证书" width="110" align="center">
          <template #default="{ row }">
            <el-link type="primary" @click="openSSL(row)">
              <span v-if="row.ssl_days >= 0" :class="{ 'ssl-soon': row.ssl_days <= 14 }">{{ row.ssl_status }}</span>
              <span v-else class="ssl-undeployed">{{ row.ssl_status || '未部署' }}</span>
            </el-link>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="130">
          <template #default="{ row }">
            <el-dropdown
              v-if="row.active === 'running' || row.active === 'stopped'"
              trigger="hover"
              @command="cmd => doStateAction(row, cmd)"
            >
              <el-tag size="small" :type="row.active === 'running' ? 'success' : 'info'" class="state-tag">
                {{ row.active === 'running' ? '运行中' : '已停止' }}
                <el-icon class="state-caret"><ArrowDown /></el-icon>
              </el-tag>
              <template #dropdown>
                <el-dropdown-menu>
                  <template v-if="row.active === 'running'">
                    <el-dropdown-item command="stop">停止</el-dropdown-item>
                    <el-dropdown-item command="restart" divided>重启</el-dropdown-item>
                  </template>
                  <el-dropdown-item v-else command="start">启动</el-dropdown-item>
                </el-dropdown-menu>
              </template>
            </el-dropdown>
            <el-tag v-else size="small" type="info">未知</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" min-width="270" fixed="right">
          <template #default="{ row }">
            <div class="ops-cell">
              <el-button size="small" type="primary" link @click="openSettings(row)">设置</el-button>
              <el-button size="small" type="info" link @click="openLogs(row)">日志</el-button>
              <el-button size="small" type="success" link @click="openStat(row)">统计</el-button>
              <el-button size="small" type="warning" link @click="openSecurity(row)">安全</el-button>
              <el-button size="small" type="danger" link @click="remove(row)">删除</el-button>
            </div>
          </template>
        </el-table-column>
      </el-table>

      </el-card>

    <!-- 环境管理弹窗（面板式：PHP 完整管理 + Python/Node/Go 现代化） -->
    <EnvManagerDialog
      v-model="envManagerVisible"
      :env-name="activeEnvName"
      :versions="currentRuntimeVersions"
      @refresh="refreshEnvStatus"
    />

    <!-- 默认页面抽屉（4 个全局默认页 + 默认站点设置） -->
    <DefaultPagesDrawer ref="defaultPagesRef" @refresh="loadSites" />

    <!-- 创建网站对话框（按类型动态填参） -->
    <el-dialog v-model="createVisible" :title="`创建${typeMeta[form.type]?.label || ''}网站`" :width="dialogWidth" :top="isMobile ? '4vh' : '5vh'" class="site-create-dialog">
      <el-form :model="form" :label-position="labelPosition" label-width="110px" :rules="rules" ref="formRef">
        <el-form-item label="站点类型" prop="type">
          <el-radio-group v-model="form.type" @change="onTypeChange">
            <el-radio-button value="static">纯静态</el-radio-button>
            <el-radio-button value="php">PHP</el-radio-button>
            <el-radio-button value="python">Python</el-radio-button>
            <el-radio-button value="node">Node</el-radio-button>
            <el-radio-button value="go">Go</el-radio-button>
            <el-radio-button value="proxy">反向代理</el-radio-button>
          </el-radio-group>
        </el-form-item>

        <el-form-item label="域名 / IP" prop="domain">
          <el-input v-model="form.domain" placeholder="如 example.com 或 1.2.3.4（必填）" />
          <span class="tip">支持 *.example.com 通配符；多个域名用逗号分隔，主域名作为网站名称</span>
        </el-form-item>
        <el-form-item label="网站名称" prop="name">
          <el-input v-model="form.name" placeholder="留空自动使用完整域名" maxlength="64" />
          <span class="tip">创建后可在列表点击「名称」直接修改</span>
        </el-form-item>
        <el-form-item label="监听端口" prop="port">
          <el-input-number v-model="form.port" :min="1" :max="65535" />
          <span class="tip">对外访问端口</span>
        </el-form-item>

        <!-- 纯静态 -->
        <template v-if="form.type === 'static'">
          <el-form-item label="根目录" prop="root">
            <el-input v-model="form.root" :placeholder="rootPlaceholder" />
            <span class="tip">默认 /www/wwwroot/&lt;域名第一段&gt;</span>
          </el-form-item>
        </template>

        <!-- PHP：版本 + 根目录 + 可选建库/建FTP -->
        <template v-else-if="form.type === 'php'">
          <el-form-item label="PHP 版本" prop="runtime_version">
            <el-select v-model="form.runtime_version" placeholder="选择 PHP 版本" style="width: 100%" :disabled="!runtimeOptions.php.length">
              <el-option v-for="v in runtimeOptions.php" :key="v.value" :label="v.label" :value="v.value" />
              <template v-if="!runtimeOptions.php.length">
                <el-option label="未检测到已安装的 PHP，请先在应用商店安装" value="" disabled />
              </template>
            </el-select>
          </el-form-item>
          <el-form-item label="根目录" prop="root">
            <el-input v-model="form.root" :placeholder="rootPlaceholder" />
            <span class="tip">默认 /www/wwwroot/&lt;域名第一段&gt;</span>
          </el-form-item>

          <el-divider content-position="left">可选：创建数据库</el-divider>
          <el-form-item label="创建数据库" prop="create_db">
            <el-switch v-model="form.create_db" :disabled="!dbAvailable" />
            <template v-if="!dbAvailable">
              <span class="tip warn">本机未安装 MySQL/MariaDB，请先在「应用商店」安装后再启用</span>
              <el-button
                size="small"
                type="primary"
                link
                :loading="installing === 'mysql' || installing === 'mariadb'"
                @click="quickInstallDb"
              >一键安装</el-button>
            </template>
          </el-form-item>
          <template v-if="form.create_db">
            <el-form-item label="数据库名" prop="db_name">
              <el-input v-model="form.db_name" placeholder="留空自动按域名第一段 + 6 位随机生成" />
            </el-form-item>
            <el-form-item label="数据库用户" prop="db_user">
              <el-input v-model="form.db_user" placeholder="留空默认与数据库同名" />
            </el-form-item>
            <el-form-item label="数据库密码" prop="db_password">
              <el-input v-model="form.db_password" type="password" show-password placeholder="留空自动生成 12 位随机密码">
                <template #append>
                  <el-button @click="regenDbPassword">重新生成</el-button>
                </template>
              </el-input>
            </el-form-item>
          </template>

          <el-divider content-position="left">可选：创建 FTP</el-divider>
          <el-form-item label="创建 FTP" prop="create_ftp">
            <el-switch v-model="form.create_ftp" :disabled="!ftpAvailable" />
            <template v-if="!ftpAvailable">
              <span class="tip warn">本机未安装 vsftpd，请先在「应用商店」安装后再启用</span>
              <el-button
                size="small"
                type="primary"
                link
                :loading="installing === 'ftp'"
                @click="quickInstallFtp"
              >一键安装</el-button>
            </template>
          </el-form-item>
          <template v-if="form.create_ftp">
            <el-form-item label="FTP 用户名" prop="ftp_username">
              <el-input v-model="form.ftp_username" placeholder="留空自动按域名第一段 + 6 位随机生成" />
            </el-form-item>
            <el-form-item label="FTP 密码" prop="ftp_password">
              <el-input v-model="form.ftp_password" type="password" show-password placeholder="留空自动生成 12 位随机密码">
                <template #append>
                  <el-button @click="regenFtpPassword">重新生成</el-button>
                </template>
              </el-input>
            </el-form-item>
          </template>
        </template>

        <!-- Python：版本 + 路径 + 框架 + 启动命令 + 端口 -->
        <template v-else-if="form.type === 'python'">
          <el-form-item label="Python 版本" prop="runtime_version">
            <el-select v-model="form.runtime_version" placeholder="选择 Python 版本" style="width: 100%" :disabled="!runtimeOptions.python.length">
              <el-option v-for="v in runtimeOptions.python" :key="v.value" :label="v.label" :value="v.value" />
              <template v-if="!runtimeOptions.python.length">
                <el-option label="未检测到已安装的 Python3，请先在应用商店安装" value="" disabled />
              </template>
            </el-select>
          </el-form-item>
          <el-form-item label="项目路径" prop="root">
            <el-input v-model="form.root" placeholder="如 /www/wwwroot/站点名（留空自动创建）" />
          </el-form-item>
          <el-form-item label="框架" prop="framework">
            <el-select v-model="form.framework" style="width: 100%">
              <el-option label="通用（自定义启动命令）" value="generic" />
              <el-option label="Flask" value="flask" />
              <el-option label="Django" value="django" />
            </el-select>
          </el-form-item>
          <el-form-item label="启动命令" prop="start_command">
            <el-input v-model="form.start_command" :placeholder="frameworkHint" />
            <span class="tip">将由 systemd 守护运行</span>
          </el-form-item>
          <el-form-item label="项目端口" prop="proxy_port">
            <el-input-number v-model="form.proxy_port" :min="1" :max="65535" />
            <span class="tip">站点将反代到 127.0.0.1:{{ form.proxy_port }}</span>
          </el-form-item>
          <el-form-item label="环境变量">
            <el-input v-model="form.env_vars" type="textarea" :rows="2" placeholder="KEY=VALUE，每行一个，可选" />
          </el-form-item>
        </template>

        <!-- Node -->
        <template v-else-if="form.type === 'node'">
          <el-form-item label="Node 版本" prop="runtime_version">
            <el-select v-model="form.runtime_version" placeholder="选择 Node 版本" style="width: 100%" :disabled="!runtimeOptions.node.length">
              <el-option v-for="v in runtimeOptions.node" :key="v.value" :label="v.label" :value="v.value" />
              <template v-if="!runtimeOptions.node.length">
                <el-option label="未检测到已安装的 Node.js，请先在应用商店安装" value="" disabled />
              </template>
            </el-select>
          </el-form-item>
          <el-form-item label="项目路径" prop="root">
            <el-input v-model="form.root" placeholder="如 /www/wwwroot/站点名（留空自动创建）" />
          </el-form-item>
          <el-form-item label="启动命令" prop="start_command">
            <el-input v-model="form.start_command" placeholder="如 npm run start / node app.js" />
            <span class="tip">将由 systemd 守护运行</span>
          </el-form-item>
          <el-form-item label="项目端口" prop="proxy_port">
            <el-input-number v-model="form.proxy_port" :min="1" :max="65535" />
            <span class="tip">站点将反代到 127.0.0.1:{{ form.proxy_port }}</span>
          </el-form-item>
          <el-form-item label="环境变量">
            <el-input v-model="form.env_vars" type="textarea" :rows="2" placeholder="KEY=VALUE，每行一个，可选" />
          </el-form-item>
        </template>

        <!-- Go -->
        <template v-else-if="form.type === 'go'">
          <el-form-item label="Go 版本" prop="runtime_version">
            <el-select v-model="form.runtime_version" placeholder="选择 Go 版本" style="width: 100%" :disabled="!runtimeOptions.go.length">
              <el-option v-for="v in runtimeOptions.go" :key="v.value" :label="v.label" :value="v.value" />
              <template v-if="!runtimeOptions.go.length">
                <el-option label="未检测到已安装的 Golang，请先在应用商店安装" value="" disabled />
              </template>
            </el-select>
          </el-form-item>
          <el-form-item label="项目路径" prop="root">
            <el-input v-model="form.root" placeholder="如 /www/wwwroot/站点名（留空自动创建）" />
          </el-form-item>
          <el-form-item label="启动命令" prop="start_command">
            <el-input v-model="form.start_command" placeholder="如 ./app / go run main.go" />
            <span class="tip">将由 systemd 守护运行</span>
          </el-form-item>
          <el-form-item label="项目端口" prop="proxy_port">
            <el-input-number v-model="form.proxy_port" :min="1" :max="65535" />
            <span class="tip">站点将反代到 127.0.0.1:{{ form.proxy_port }}</span>
          </el-form-item>
          <el-form-item label="环境变量">
            <el-input v-model="form.env_vars" type="textarea" :rows="2" placeholder="KEY=VALUE，每行一个，可选" />
          </el-form-item>
        </template>

        <!-- 反向代理 -->
        <template v-else-if="form.type === 'proxy'">
          <el-form-item label="代理目标" prop="proxy_pass">
            <el-input v-model="form.proxy_pass" placeholder="如 http://127.0.0.1:3000 或 https://api.example.com" />
          </el-form-item>
        </template>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submit">创建</el-button>
      </template>
    </el-dialog>

    <!-- 网站设置抽屉：success 只更新对应站点行的域名字段，不重新拉整个列表 -->
    <SiteSettings ref="settingsRef" @success="onSiteSettingsSaved" />

    <!-- 访问日志弹窗（结构化展示 + 分析） -->
    <SiteLogsDialog ref="logsRef" />

    <!-- 删除网站确认弹窗（打钩选择要删除的内容） -->
    <el-dialog v-model="delVisible" title="删除网站" width="420px" class="site-delete-dialog">
      <div class="del-warning">
        <el-icon class="del-warning-icon"><WarningFilled /></el-icon>
        <span>即将删除网站「<b>{{ delSite?.name }}</b>」，请勾选要删除的内容：</span>
      </div>
      <div class="del-options">
        <el-checkbox v-model="delOptions.conf" disabled>网站配置（必删：Web 服务器配置/进程服务）</el-checkbox>
        <el-checkbox v-if="delSite && delSite.type !== 'proxy'" v-model="delOptions.root">站点目录（{{ delSite.root }}）</el-checkbox>
        <el-checkbox v-if="delSite && delSite.type === 'php'" v-model="delOptions.db">数据库（{{ delSite.name }}）</el-checkbox>
        <el-checkbox v-if="delSite && delSite.type === 'php'" v-model="delOptions.ftp">FTP 用户（{{ delSite.name }}）</el-checkbox>
      </div>
      <template #footer>
        <el-button @click="delVisible = false">取消</el-button>
        <el-button type="danger" :loading="delSubmitting" @click="confirmDelete">确认删除</el-button>
      </template>
    </el-dialog>

    <!-- 站点访问统计弹窗 -->
    <SiteStatDialog v-model="statVisible" :site-id="statSiteId" :site-name="statSiteName" />
    <SiteSecurityDialog v-model="securityVisible" :site-id="securitySiteId" :site-name="securitySiteName" />
    <SiteSSLDialog v-model="sslVisible" :site="sslSite" @success="loadSites" />

    <!-- 域名下拉：Teleport 到 body 避免被 el-table overflow 裁切 -->
    <Teleport to="body">
      <div
        v-if="domainListVisible"
        class="domain-list-wrap"
        :style="domainListStyle"
        @mouseenter="keepDomainListOpen"
        @mouseleave="closeDomainList"
      >
        <div class="domain-list">
          <div
            v-for="d in domainListDomains"
            :key="d"
            class="domain-list-row"
            @click="visitDomain(d, domainListRow)"
          >
            <span class="domain-text">{{ d }}</span>
            <el-button class="copy-btn" size="small" text :title="`复制域名 ${d}`" @click="copyDomain(d)">
              <el-icon><CopyDocument /></el-icon>
            </el-button>
            <DomainQrcode :domain="d" :port="domainListRow.port" :is-https="!!domainListRow.ssl_enabled" />
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onBeforeUnmount, nextTick, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useRouter } from 'vue-router'
import { Edit, ArrowDown, WarningFilled, Warning, Download, Tools, Setting, Delete, CopyDocument, Files } from '@element-plus/icons-vue'
import request from '../utils/request'
import SiteSettings from './SiteSettings.vue'
import SiteStatDialog from '../components/SiteStatDialog.vue'
import SiteSecurityDialog from '../components/SiteSecurityDialog.vue'
import SiteLogsDialog from '../components/SiteLogsDialog.vue'
import SiteSSLDialog from '../components/SiteSSLDialog.vue'
import EnvManagerDialog from '../components/EnvManagerDialog.vue'
import DefaultPagesDrawer from '../components/DefaultPagesDrawer.vue'
import DomainQrcode from '../components/DomainQrcode.vue'
import { useNavStore } from '@/stores/nav'
import { useInstallTrackerStore } from '../stores/installTracker'

const router = useRouter()
const nav = useNavStore()
const sites = ref([])
const settingsRef = ref()
const logsRef = ref()
const loading = ref(true)
// 统计弹窗状态
const statVisible = ref(false)
const statSiteId = ref(0)
const statSiteName = ref('')
const sslVisible = ref(false)
const sslSite = ref({})
// 单站安全弹窗状态
const securityVisible = ref(false)
const securitySiteId = ref(0)
const securitySiteName = ref('')
const createVisible = ref(false)
// 运行时环境管理弹窗
const envManagerVisible = ref(false)
// 当前语言默认管理的运行环境名（第一个已安装版本）
const activeEnvName = computed(() => {
  const list = currentRuntimeVersions.value
  const installed = list.find(v => v.installed)
  return (installed || list[0] || {}).name || ''
})
const openEnvManager = () => {
  // 弹窗立即弹出；环境状态后台异步刷新（不阻塞 UI）。
  // onOpen() 内部会重新调 loadEnvInfo 拉取当前 envName 的详情，
  // 而左侧 versions 列表在后台 loadRuntimes() 返回后会自动更新。
  // 这样不依赖 /env/status 和 /apps/list 串行响应时间，弹窗秒开。
  envManagerVisible.value = true
  refreshEnvStatus().catch(() => { /* 静默，后台跑就行 */ })
}
const submitting = ref(false)
const formRef = ref()

// Web 服务器环境状态（nginx / apache）
const nginxInstalled = ref(true)
const nginxEnv = ref({})
const installingNginx = ref(false)
const pendingActionMap = ref({}) // {nginx:'install'|'uninstall'|''}
const nginxInstallTimer = ref(null)
// 弹窗内各类型环境状态与一键安装
const envStatusMap = ref({})
const installing = ref('')
const selectedVersionMap = ref({})
// 下拉框当前显示的版本：跟随 activeTab 切换各运行时独立记忆。
// - getter：当前 Tab 用户曾选过的版本集合 > 当前 Tab 缺失版本列表中的全部 > 空数组
// - setter：用户改动多选框时把所有选中的 app key 写回 selectedVersionMap（按 Tab 隔离记忆）
const selectedRuntimeVersion = computed({
  get() {
    if (!['php', 'python', 'go', 'node'].includes(activeTab.value)) return []
    const keys = runtimeAppKey[activeTab.value]
    const keyArray = Array.isArray(keys) ? keys : [keys]
    // 只返回用户明确勾选的版本，默认不预勾选任何版本
    return keyArray.filter(k => selectedVersionMap.value[k])
  },
  set(v) {
    if (Array.isArray(v)) {
      const active = activeTab.value
      if (!['php', 'python', 'go', 'node'].includes(active)) return
      const keyArray = Array.isArray(runtimeAppKey[active]) ? runtimeAppKey[active] : [runtimeAppKey[active]]
      // 只记录当前 Tab 涉及的 key；取消勾选的 key 从 map 中移除
      for (const k of keyArray) {
        if (v.includes(k)) selectedVersionMap.value[k] = k
        else delete selectedVersionMap.value[k]
      }
    }
  }
})
const runtimeInstallKey = computed(() => {
  if (!['php', 'python', 'go', 'node'].includes(activeTab.value)) return []
  if (!runtimeMissing.value?._versions?.length) return []
  // 默认不选中任何版本，只安装用户明确勾选的版本
  return selectedRuntimeVersion.value
})
// 当前 Tab 是否正在安装某个运行时版本（仅用于按钮 loading 显示）。
// 语义独立于「勾选的版本」：只要 installing 非空且属于当前 Tab 的 runtime key 即视为安装中，
// 避免与 runtimeInstallKey（勾选版本）耦合，杜绝「未装 PHP 却显示加载中」的竞态残留。
const isInstallingCurrentRuntime = computed(() => {
  if (!installing.value) return false
  const keys = runtimeAppKey[activeTab.value]
  if (!keys) return false
  return keys.includes(installing.value)
})
// 当前正在装的 key 对应的中文标签（用于多选装多个时提示进度）
const currentInstallingLabel = computed(() => {
  if (!installing.value) return ''
  const meta = envStatusMap.value[installing.value]
  return meta?.name || installing.value
})
// 弹窗宽度 / label 对齐方式：根据视口响应式（移动端避免超出屏幕）
const isMobile = ref(false)
const dialogWidth = computed(() => isMobile.value ? '92vw' : '620px')
const labelPosition = computed(() => isMobile.value ? 'top' : 'left')
const updateResponsive = () => {
  isMobile.value = window.innerWidth < 768
}
const TAB_STORAGE_KEY = 'website_active_tab'
const activeTab = ref(localStorage.getItem(TAB_STORAGE_KEY) || 'all')
const runtimes = ref([]) // 应用商店已安装的运行环境（用于版本下拉）

// 类型元信息（标签名 + Tag 颜色）
const typeMeta = {
  static: { label: '纯静态', tag: 'success' },
  php: { label: 'PHP', tag: 'warning' },
  python: { label: 'Python', tag: 'primary' },
  go: { label: 'Go', tag: 'danger' },
  node: { label: 'Node', tag: 'info' },
  proxy: { label: '反向代理', tag: 'warning' }
}

// 站点类型 -> 应用商店 app key（php 支持多版本共存，匹配所有 php* 应用）
const runtimeAppKey = {
  php: ['php56', 'php70', 'php71', 'php72', 'php73', 'php74', 'php80', 'php81', 'php82', 'php83', 'php84', 'php'],
  python: ['python3', 'python38', 'python39', 'python310', 'python311', 'python312', 'python313'],
  node: ['nodejs', 'node14', 'node16', 'node18', 'node20', 'node22', 'node24'],
  go: ['golang', 'go119', 'go120', 'go121', 'go122', 'go123', 'go124', 'go125']
}

// activeTab 对应的运行时环境 key 和状态
// 支持多版本：如果任一版本已安装则返回聚合状态
const currentRuntimeEnv = computed(() => {
  const keys = runtimeAppKey[activeTab.value]
  if (!keys) return null
  const keyArray = Array.isArray(keys) ? keys : [keys]
  // 检查是否有任一版本已安装
  const installed = keyArray.find(k => envStatusMap.value[k]?.installed)
  if (installed) {
    return envStatusMap.value[installed]
  }
  // 都没有安装，返回默认版本的状态（用于显示未安装提示）
  return envStatusMap.value[keyArray[0]] || null
})

// 当前标签页对应的所有运行环境版本列表（用于版本管理卡片）
const currentRuntimeVersions = computed(() => {
  const keys = runtimeAppKey[activeTab.value]
  if (!keys) return []
  const keyArray = Array.isArray(keys) ? keys : [keys]
  return keyArray.map(k => {
    const meta = envStatusMap.value[k]
    if (!meta) return null
    // 系统发行版自带的 runtime（php / python3 / golang / nodejs）卸载会破坏系统组件，
    // 不显示「卸载」按钮，避免误操作
    const systemDefaultKeys = ['php', 'python3', 'golang', 'nodejs']
    return {
      key: k,
      name: meta.name,
      installed: meta.installed,
      uninstallable: !systemDefaultKeys.includes(k),
      version: meta.version,
      versions: meta.versions,
      versionDefault: meta.version_default,
      remarks: meta.remarks,
      // 来自 /apps/list 的运行时状态（not_installed / installing / queued / installed / uninstalling / failed）
      // 用于 EnvManagerDialog 左侧下拉框里显示「安装中 / 队列中」状态
      status: (runtimes.value || []).find(a => a.key === k)?.status || (meta.installed ? 'installed' : 'not_installed')
    }
  }).filter(Boolean)
})

// 当前标签页运行时环境是否缺失（用于列表页遮罩）
// PHP / Python / Go / Node 均为多版本，列出所有未安装版本供下拉选择
const runtimeMissing = computed(() => {
  if (!['php', 'python', 'go', 'node'].includes(activeTab.value)) return null
  const keys = runtimeAppKey[activeTab.value]
  const keyArray = Array.isArray(keys) ? keys : [keys]

  // Python / Node / Go 的系统默认运行时（python3 / nodejs / golang）不算"已安装"
  // 创建网站需要面板管理的多版本环境
  let checkKeys = keyArray
  if (activeTab.value === 'python') {
    checkKeys = keyArray.filter(k => k !== 'python3')
  } else if (activeTab.value === 'node') {
    checkKeys = keyArray.filter(k => k !== 'nodejs')
  } else if (activeTab.value === 'go') {
    checkKeys = keyArray.filter(k => k !== 'golang')
  }

  const allMetas = checkKeys.map(k => envStatusMap.value[k]).filter(Boolean)
  if (allMetas.length === 0) return null

  // 只要任一版本已安装，即视为环境可用（否则为缺失）
  const installed = allMetas.find(m => m.installed)
  if (installed) return null

  const missingMetas = allMetas.filter(m => !m.installed)
  if (missingMetas.length === 0) return null

  const labelMap = { php: 'PHP', python: 'Python', go: 'Go', node: 'Node.js' }
  const first = missingMetas[0]
  return {
    ...first,
    key: first.key,
    name: labelMap[activeTab.value],
    remarks: `未检测到 ${labelMap[activeTab.value]} 环境，请选择版本安装`,
    _versions: missingMetas
  }
})

const tabs = computed(() => {
  const list = [
    { key: 'all', label: '全部', badgeType: 'primary' },
    { key: 'php', label: 'PHP', badgeType: 'warning' },
    { key: 'static', label: '纯静态', badgeType: 'success' },
    { key: 'python', label: 'Python', badgeType: 'primary' },
    { key: 'go', label: 'Go', badgeType: 'danger' },
    { key: 'node', label: 'Node', badgeType: 'info' },
    { key: 'proxy', label: '反向代理', badgeType: 'warning' }
  ]
  return list.map(t => ({
    ...t,
    count: t.key === 'all' ? sites.value.length : sites.value.filter(s => s.type === t.key).length
  }))
})

const filteredSites = computed(() => {
  if (activeTab.value === 'all') return sites.value
  return sites.value.filter(s => s.type === activeTab.value)
})

function onTabChange() {
  localStorage.setItem(TAB_STORAGE_KEY, activeTab.value)
  loadSites()

  // 切换到 PHP/Python/Go/Node 等需要选择版本的运行时而当前未安装时，
  // 如果用户还没有勾选任何版本，自动为他选中第一个推荐版本，避免
  // "未检测到环境但一键安装按钮点不了" 的尴尬状态。
  const type = activeTab.value
  const keys = runtimeAppKey[type]
  if (keys && keys.length) {
    const hasSelected = keys.some(k => selectedVersionMap.value[k])
    if (!hasSelected) {
      // 默认勾选「最新版本」：从 _versions（实际下拉选项，python/node/go 已过滤掉系统默认版）
      // 取最后一个，即最新版本（node24 / python313 / go125 / php84）。
      // 值统一存 key 本身（与 setter 一致），不再存版本号字符串，避免 getter 解析歧义。
      const versions = runtimeMissing.value?._versions
      const latest = versions?.[versions.length - 1]
      if (latest?.key) {
        selectedVersionMap.value[latest.key] = latest.key
      }
    }
  }
}

// 已安装运行环境版本选项（从 envStatus 读取多版本信息）
const runtimeOptions = computed(() => {
  const opts = { php: [], python: [], node: [], go: [] }
  for (const [key, meta] of Object.entries(envStatusMap.value)) {
    if (!meta.installed) continue
    const t = Object.keys(runtimeAppKey).find(k => {
      const v = runtimeAppKey[k]
      return Array.isArray(v) ? v.includes(key) : v === key
    })
    if (!t) continue
    const ver = cleanVer(t, meta.version || meta.name)
    if (ver) opts[t].push({ value: ver, label: ver })
  }
  return opts
})

function cleanVer(type, raw) {
  if (!raw) return ''
  if (type === 'php') {
    // 只取主.次版本（如 PHP 8.2），后端 ensureRuntime 用 phpMinorVersion 归约，
    // 若传完整版 PHP 8.2.33 会导致格式与选择项不一致
    const m = raw.match(/PHP\s+\d+\.\d+/)
    return m ? m[0] : raw.split('\n')[0]
  }
  if (type === 'node') {
    const first = raw.split('\n')[0].trim()
    return first.startsWith('v') ? 'Node ' + first : first
  }
  if (type === 'python') {
    const first = raw.split('\n')[0].trim()
    return first.startsWith('Python') ? first : 'Python ' + first
  }
  if (type === 'go') {
    const first = raw.split('\n')[0].trim()
    return first.startsWith('go') ? 'Go ' + first.replace('go version ', '').split(' ')[0] : first
  }
  return raw.split('\n')[0].trim()
}

const form = reactive({
  name: '',
  domain: '',
  port: 80,
  type: 'static',
  root: '',
  runtime_version: '',
  start_command: '',
  env_vars: '',
  proxy_port: 3000,
  proxy_pass: '',
  framework: 'generic',
  create_db: false,
  db_name: '',
  db_user: '',
  db_password: '',
  create_ftp: false,
  ftp_username: '',
  ftp_password: '',
  remark: ''
})

const rules = {
  domain: [{ required: true, message: '请输入域名', trigger: 'blur' }],
  port: [{ required: true, message: '请输入端口', trigger: 'blur' }],
  type: [{ required: true, message: '请选择类型', trigger: 'change' }]
}

// 启动命令占位提示（按框架）
const frameworkHint = computed(() => {
  if (form.type !== 'python') return ''
  switch (form.framework) {
    case 'flask': return '如 gunicorn -w 2 -b 127.0.0.1:' + form.proxy_port + ' app:app'
    case 'django': return '如 gunicorn -w 2 -b 127.0.0.1:' + form.proxy_port + ' myproject.wsgi'
    default: return '如 python app.py'
  }
})

// ====== 创建网站辅助：根目录/数据库/FTP 自动填充 & 安装检测 ======

// 域名（如 "vltphp.n.05v.cn" → "vltphp"）作为网站目录前缀。
// domain 可能形如 "host:port"（剥端口），含中文/特殊字符 → 仅保留 [a-z0-9-_]，
// 若最终为空则回退到 fallback（站点名），再次为空则回退 "site"。
function sanitizeDomainPrefix(domain, fallback) {
  let name = (fallback || '').trim()
  if (domain) {
    let d = domain.trim().split(':')[0]
    if (d.includes('.')) d = d.split('.')[0]
    d = d.toLowerCase().trim()
    if (d) name = d
  }
  const cleaned = name.replace(/[^a-z0-9_-]/gi, '').toLowerCase()
  return cleaned || 'site'
}

// 根目录预览：基于当前 domain / name
const rootPreview = computed(() => {
  return '/www/wwwroot/' + sanitizeDomainPrefix(form.domain, form.name)
})
// root 当前是不是由我们自动推断？用户手动改过则视为"已锁定"，不再覆盖
const rootAuto = ref(false)
// root 字段 placeholder：让用户清楚默认推断规则
const rootPlaceholder = computed(() =>
  `留空默认 ${rootPreview.value}（根据域名截取，或未填域名回退到站点名）`
)

// n 位 [a-z0-9] 随机串（用于 db 名 / ftp 用户名后缀）
function rand(n) {
  const cs = 'abcdefghijklmnopqrstuvwxyz0123456789'
  let s = ''
  for (let i = 0; i < n; i++) s += cs[Math.floor(Math.random() * cs.length)]
  return s
}
// n 位 [A-Za-z0-9] 随机密码（用于 DB / FTP 密码）
function genPwd(n = 12) {
  const cs = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789'
  let s = ''
  for (let i = 0; i < n; i++) s += cs[Math.floor(Math.random() * cs.length)]
  return s
}
// 自动生成 db_name / db_user / ftp_username：域名第一段（如 vltphp.n.05v.cn → vltphp）+ "_" + 6 位随机
function autoDbName() {
  const base = sanitizeDomainPrefix(form.domain, form.name)
  return `${base}_${rand(6)}`
}
function autoFtpUsername() {
  const base = sanitizeDomainPrefix(form.domain, form.name)
  return `${base}_${rand(6)}`
}

// 数据库 / FTP 服务本机是否已安装
const dbAvailable = computed(() => {
  return ['mysql', 'mariadb'].some(k =>
    runtimes.value.some(a => a.key === k && a.status === 'installed')
  )
})
const ftpAvailable = computed(() => {
  return runtimes.value.some(a => a.key === 'ftp' && a.status === 'installed')
})

// 监听 domain / name → 自动覆盖 root（前提：root 为空或仍是上次自动值）
watch(() => form.domain, () => {
  if (!form.root || rootAuto.value) {
    form.root = rootPreview.value
    rootAuto.value = true
  }
})
watch(() => form.name, (newName, oldName) => {
  // 站点名改：若用户没改过 root，一并更新
  if (!form.root || rootAuto.value) {
    form.root = rootPreview.value
    rootAuto.value = true
  }
  // 名称变化时若已开启建库/FTP，自动刷一次 db_name / db_user / ftp_username（密码不动，密码用户可手动重新生成）
  if (oldName !== undefined) {
    if (form.create_db) form.db_name = autoDbName()
    if (form.create_ftp) form.ftp_username = autoFtpUsername()
  }
})
// 监听 root 自身：用户手动改后锁定自动覆盖
watch(() => form.root, (val) => {
  if (val && val !== rootPreview.value) {
    rootAuto.value = false
  } else if (val && val === rootPreview.value) {
    rootAuto.value = true
  }
})

// 监听 create_db：开启时自动填充 db_name/db_user/db_password（已有值不覆盖）
watch(() => form.create_db, (v) => {
  if (v) {
    if (!form.db_name) form.db_name = autoDbName()
    if (!form.db_user) form.db_user = form.db_name
    if (!form.db_password) form.db_password = genPwd(12)
  }
})
// 监听 create_ftp：开启时自动填充 ftp_username/ftp_password（已有值不覆盖）
watch(() => form.create_ftp, (v) => {
  if (v) {
    if (!form.ftp_username) form.ftp_username = autoFtpUsername()
    if (!form.ftp_password) form.ftp_password = genPwd(12)
  }
})

// 一键重新生成 12 位密码（按钮直接调）
function regenDbPassword() {
  form.db_password = genPwd(12)
}
function regenFtpPassword() {
  form.ftp_password = genPwd(12)
}

// 未安装时一键安装（数据库/FTP/运行时环境）。支持单 key 字符串或 key 数组（多版本批量装）
async function quickInstall(keyOrKeys, label) {
  const keyList = (Array.isArray(keyOrKeys) ? keyOrKeys : [keyOrKeys]).filter(Boolean)
  if (!keyList.length) return
  try {
    await ElMessageBox.confirm(
      keyList.length > 1
        ? `将自动在「应用商店」依次安装「${label}」的 ${keyList.length} 个版本。每个版本安装完成后才开始下一个，请耐心等待。`
        : `将自动在「应用商店」开始安装「${label}」。安装可能需要几分钟，请耐心等待。`,
      `一键安装 ${label}`,
      { confirmButtonText: '开始安装', cancelButtonText: '取消', type: 'info' }
    )
  } catch { return }

  let activeKey = keyList[0]
  // watchdog 启动前先发安装请求；失败时直接退出，不留任何 installing 残留
  try {
    const payload = { key: activeKey }
    const meta = envStatusMap.value[activeKey]
    if (meta?.select_version) {
      const chosen = selectedVersionMap.value[activeKey] || meta.version_default
      if (chosen) payload.version = chosen
    }
    await request.post('/apps/install', payload)
    const tracker = useInstallTrackerStore()
    tracker.unmarkRemoved(activeKey)
    tracker.upsert({ key: activeKey, name: meta?.name || activeKey, action: 'install', status: 'queued', message: '排队等待中...' })
    ElMessage.success('已提交安装，请稍候...')
  } catch (e) {
    ElMessage.error('安装失败：' + (e?.msg || e?.message || '未知错误'))
    return
  }

  // 启动 watchdog：拉到 installed 装下一个；拉到 failed 终止；连续 90s 状态一直 installing 且无变化 → 报警
  installing.value = activeKey
  let lastStatus = ''
  let stuckSince = Date.now()
  const watchTimer = setInterval(async () => {
    try {
      const res = await request.get('/apps/list')
      const apps = res.data || []
      const app = apps.find((a) => a.key === activeKey)
      if (!app) return
      // 状态变化 → 重置 stuck 计时
      if (app.status !== lastStatus) {
        lastStatus = app.status
        stuckSince = Date.now()
      }
      // 卡死检测：连续 90 秒状态停在同一个 installing 不动 → 后端可能 ghost
      if (app.status === 'installing' && Date.now() - stuckSince > 90000) {
        clearInterval(watchTimer)
        installing.value = ''
        ElMessage.warning(`${activeKey} 安装状态长时间未变化，可能后台已异常，请到应用商店查看`)
        return
      }
      if (app.status === 'installed') {
        // 当前版本装完 -> 装下一个
        const idx = keyList.indexOf(activeKey)
        if (idx >= 0 && idx < keyList.length - 1) {
          activeKey = keyList[idx + 1]
          installing.value = activeKey
          lastStatus = ''
          stuckSince = Date.now()
          try {
            const payload = { key: activeKey }
            const meta = envStatusMap.value[activeKey]
            if (meta?.select_version) {
              const chosen = selectedVersionMap.value[activeKey] || meta.version_default
              if (chosen) payload.version = chosen
            }
            await request.post('/apps/install', payload)
            const tracker = useInstallTrackerStore()
            tracker.unmarkRemoved(activeKey)
            tracker.upsert({ key: activeKey, name: meta?.name || activeKey, action: 'install', status: 'queued', message: '排队等待中...' })
          } catch (e) {
            clearInterval(watchTimer)
            installing.value = ''
            ElMessage.error('安装失败：' + (e?.msg || e?.message || '未知错误'))
          }
        } else {
          // 全部装完
          clearInterval(watchTimer)
          installing.value = ''
          ElMessage.success(`${label} 全部安装完成（${keyList.length} 个版本）`)
          await loadEnvStatus()
          await loadRuntimes()
        }
      } else if (app.status === 'failed') {
        clearInterval(watchTimer)
        installing.value = ''
        ElMessage.error(`${activeKey} 安装失败，请查看日志`)
      }
    } catch (e) {}
  }, 3000)
}

// 页面进入时接管后台正在跑的任务：从 /apps/list 找 status=installing/failed 且 EnvStatus 还没显示装好的 runtime key，
// 自动启动 watchdog 跟踪进度——避免用户刷新页面后「安装中」提示消失，或者反过来卡住永远转圈。
async function resumePendingInstalls(apps) {
  try {
    // 复用 loadRuntimes 已拉取的 /apps/list 结果，避免重复请求
    const list = apps || runtimes.value || []
    // 只接管「真正正在安装」的任务（installing）；failed 是已失败，不该再显示 loading。
    // 且只关注运行时环境类 key（php*/python*/node*/go*），避免无关应用的失败记录触发按钮加载态。
    const runtimeKeys = new Set(Object.values(runtimeAppKey).flat())
    const pending = list.filter(a => {
      if (a.status !== 'installing') return false
      if (!runtimeKeys.has(a.key)) return false
      const env = envStatusMap.value[a.key]
      return env && !env.installed
    })
    if (!pending.length) {
      installing.value = ''
      return
    }
    const activeKey = pending[0].key
    installing.value = activeKey
    let lastStatus = ''
    let stuckSince = Date.now()
    const watchTimer = setInterval(async () => {
      try {
        const r = await request.get('/apps/list')
        const arr = r.data || []
        const app = arr.find(a => a.key === activeKey)
        if (!app) return
        if (app.status !== lastStatus) {
          lastStatus = app.status
          stuckSince = Date.now()
        }
        if (app.status === 'installing' && Date.now() - stuckSince > 90000) {
          clearInterval(watchTimer)
          installing.value = ''
          return
        }
        if (app.status === 'installed') {
          clearInterval(watchTimer)
          installing.value = ''
          await loadEnvStatus()
          await loadRuntimes()
        } else if (app.status === 'failed') {
          clearInterval(watchTimer)
          installing.value = ''
          ElMessage.error(`${activeKey} 安装失败，请查看日志`)
        }
      } catch (e) {}
    }, 3000)
  } catch (e) {}
}
function quickInstallDb() {
  // 优先装 MySQL，否则 MariaDB
  const k = runtimes.value.find(a => a.key === 'mysql' || a.key === 'mariadb')?.key || 'mysql'
  quickInstall(k, 'MySQL')
}
function quickInstallFtp() {
  quickInstall('ftp', 'vsftpd')
}

// 刷新环境状态（静默，不弹提示；供打开/关闭环境管理弹窗时后台同步数据）
async function refreshEnvStatus() {
  await loadEnvStatus()
  await loadRuntimes()
}

// 卸载运行环境
async function uninstallRuntime(ver) {
  try {
    await ElMessageBox.confirm(
      `确定卸载「${ver.name}」吗？卸载后使用该版本的网站可能无法运行。`,
      '确认卸载',
      { confirmButtonText: '卸载', cancelButtonText: '取消', type: 'warning' }
    )
  } catch { return }
  try {
    await request.post('/apps/uninstall', { key: ver.key })
    ElMessage.success('卸载已提交')
    await loadEnvStatus()
    await loadRuntimes()
  } catch (e) {
    ElMessage.error(e.response?.data?.message || '卸载失败')
  }
}

async function loadSites() {
  loading.value = true
  try {
    const res = await request.get('/site/list')
    sites.value = res.data
  } finally {
    loading.value = false
  }
}

// SiteSettings 保存成功后，原地更新对应站点的 domain/domains，
// 避免全量 loadSites 重新拉取整个列表造成的页面刷新感。
// 接收参数：{ id, domain, domains }（由 SiteSettings.vue emit 上传）
function onSiteSettingsSaved(payload) {
  if (!payload || !payload.id) return
  const row = sites.value.find(s => s.id === payload.id)
  if (!row) return
  row.domain = payload.domain || row.domain || ''
  row.domains = payload.domains || row.domains || ''
  if (payload.name !== undefined) row.name = payload.name
  if (payload.root !== undefined) row.root = payload.root
}

async function loadRuntimes() {
  try {
    const res = await request.get('/apps/list')
    runtimes.value = res.data
    // 复用本次 /apps/list 结果接管后台任务，避免页面加载时重复请求
    resumePendingInstalls(res.data)
    return res.data
  } catch (e) { /* ignore */ }
}

async function openCreate() {
  await loadEnvStatus()
  await loadRuntimes()
  const defaultType = activeTab.value === 'all' ? 'static' : activeTab.value
  Object.assign(form, {
    name: '', domain: '', port: 80, type: defaultType,
    root: '', runtime_version: '', start_command: '', env_vars: '',
    proxy_port: 3000, proxy_pass: '', framework: 'generic',
    create_db: false, db_name: '', db_user: '', db_password: '',
    create_ftp: false, ftp_username: '', ftp_password: '',
    remark: ''
  })
  // 自动带出已安装版本
  onTypeChange()
  createVisible.value = true
}

// 切换类型时自动带出已安装版本
function onTypeChange() {
  const opts = runtimeOptions.value[form.type]
  if (opts && opts.length) {
    form.runtime_version = opts[0].value
  } else {
    form.runtime_version = ''
  }
}

function buildPayload() {
  const p = { ...form }
  // 未填站点名称时用主域名作为站点名称（与后端保持一致）
  if (!p.name && p.domain) p.name = p.domain.split(',')[0].trim().replace(/^\*\./, '')
  // 备注为空时默认与站点名称一致（用户没主动填备注就跟随 name）
  if (!p.remark) p.remark = p.name
  if (form.type === 'node' || form.type === 'python' || form.type === 'go') {
    if (!p.root) p.root = `/www/wwwroot/${p.name}`
    p.proxy_pass = `http://127.0.0.1:${form.proxy_port}`
  }
  return p
}

// 复制域名到剪贴板（含降级方案）
async function copyDomain(domain) {
  if (!domain) return
  try {
    await navigator.clipboard.writeText(domain)
    ElMessage.success('域名已复制')
    return
  } catch (e) { /* 降级 */ }
  const ta = document.createElement('textarea')
  ta.value = domain
  ta.style.position = 'fixed'
  ta.style.left = '-9999px'
  ta.style.top = '-9999px'
  document.body.appendChild(ta)
  ta.focus()
  ta.select()
  try {
    const ok = document.execCommand('copy')
    ok ? ElMessage.success('域名已复制') : ElMessage.warning('复制失败，请手动复制')
  } catch (err) {
    ElMessage.warning('复制失败，请手动复制')
  } finally {
    document.body.removeChild(ta)
  }
}

async function submit() {
  await formRef.value.validate()
  // 类型相关校验
  if (form.type === 'proxy' && !form.proxy_pass) {
    ElMessage.warning('请填写代理目标')
    return
  }
  if (form.type === 'node' || form.type === 'python' || form.type === 'go') {
    if (!form.proxy_port) { ElMessage.warning('请填写项目端口'); return }
    if (!form.start_command) { ElMessage.warning('请填写启动命令'); return }
  }
  if (form.type === 'php' && form.create_db && !form.db_password) {
    ElMessage.warning('创建数据库需要填写数据库密码')
    return
  }
  if (form.type === 'php' && form.create_ftp && !form.ftp_password) {
    ElMessage.warning('创建 FTP 需要填写 FTP 密码')
    return
  }
  submitting.value = true
  try {
    await request.post('/site/create', buildPayload())
    ElMessage.success('网站创建成功')
    createVisible.value = false
    loadSites()
  } catch (e) { /* interceptor handles */ } finally {
    submitting.value = false
  }
}

function siteDir(row) {
  return row.root || `/www/wwwroot/${row.name}`
}

function openFiles(path) {
  localStorage.setItem('panel_last_file_path', path)
  // 通过 Pinia store 传递初始路径，不写入 URL，避免污染浏览器地址栏
  nav.setFilePath(path)
  router.push('/files')
}

// 站点全部绑定域名（主域名 + 附加域名，去重）
function siteAllDomains(row) {
  const list = []
  const seen = new Set()
  if (row.domain) { list.push(row.domain); seen.add(row.domain) }
  for (const d of String(row.domains || '').split(',').map(s => s.trim()).filter(Boolean)) {
    if (!seen.has(d)) { list.push(d); seen.add(d) }
  }
  return list
}

// 把泛域名 *.example.com 转换成可访问的 fan.example.com
function wildcardToVisitable(host) {
  if (host.startsWith('*.')) return 'fan.' + host.slice(2)
  return host.split(':')[0]
}

// 把 host 拆成 [hostname, portStr]；没有 :port 时返回 [host, '']
function splitHostPort(host) {
  const idx = host.lastIndexOf(':')
  // IPv6 简单忽略（业务里没有 IPv6 + 端口场景）
  if (idx <= 0) return [host, '']
  return [host.slice(0, idx), host.slice(idx + 1)]
}

// 计算某个具体域名的访问 URL
// 规则：
//   1) 域名本身已带 :port（如 127.0.0.1:8899）→ 用域名里的端口，协议按 SSL 选
//   2) 没带端口 + 启用了 SSL → https://host:443
//   3) 没带端口 + 没 SSL + row.port == 80 → http://host（默认端口省略）
//   4) 没带端口 + 没 SSL + row.port 其它   → http://host:row.port
//   5) 泛域名 *.example.com → fan.example.com
function visitHrefFor(host, row) {
  const [rawHost, hostPort] = splitHostPort(host)
  const visitHost = wildcardToVisitable(rawHost)
  const ssl = !!row.ssl_enabled

  // 域名自带端口（如 127.0.0.1:8899）：用自带端口
  if (hostPort) {
    const proto = ssl && hostPort === '443' ? 'https:' : (ssl ? 'https:' : 'http:')
    // 如果端口是 80，HTTP 时省略；443，HTTPS 时省略
    if ((proto === 'http:' && hostPort === '80') || (proto === 'https:' && hostPort === '443')) {
      return `${proto}//${visitHost}`
    }
    return `${proto}//${visitHost}:${hostPort}`
  }

  // 域名未带端口
  if (ssl) {
    // SSL 启用，访问 https
    return `https://${visitHost}`  // 默认 443
  }
  // HTTP：用 row.port，80 省略
  if (row.port === 80 || !row.port) {
    return `http://${visitHost}`
  }
  return `http://${visitHost}:${row.port}`
}

// 列表单元格点击跳转的主域名链接
function visitHref(row) {
  const host = siteAllDomains(row)[0] || location.hostname
  return visitHrefFor(host, row)
}

// 域名下拉列表（Teleport 到 body，避免被 el-table overflow 裁掉）
const domainListVisible = ref(false)
const domainListStyle = ref({ top: '0px', left: '0px' })
const domainListDomains = ref([])
const domainListRow = ref(null)
let _domainListCloseTimer = null

function openDomainList(e, row) {
  if (_domainListCloseTimer) { clearTimeout(_domainListCloseTimer); _domainListCloseTimer = null }
  if (siteAllDomains(row).length <= 1) return
  const trigger = e.currentTarget
  // 取主域名 <a> 的 left（去掉 .domain-cell 的 margin-left:-12px 偏移），
  // 这样弹层内首行文字左缘 = 上方主域名文字左缘，严格对齐
  const link = trigger.querySelector('.domain-link')
  const rect = (link || trigger).getBoundingClientRect()
  domainListStyle.value = {
    top: `${rect.bottom + 4}px`,
    left: `${rect.left - 10}px`,
  }
  domainListDomains.value = siteAllDomains(row).slice(1)
  domainListRow.value = row
  domainListVisible.value = true
}

function closeDomainList() {
  // 延迟关闭，鼠标从 trigger 移到弹层时不闪
  _domainListCloseTimer = setTimeout(() => {
    domainListVisible.value = false
    _domainListCloseTimer = null
  }, 80)
}

function keepDomainListOpen() {
  if (_domainListCloseTimer) { clearTimeout(_domainListCloseTimer); _domainListCloseTimer = null }
}
function visitDomain(host, row) {
  const url = visitHrefFor(host, row)
  window.open(url, '_blank')
}

async function openSettings(row, tab = null) {
  settingsRef.value.open(row.id, tab)
}

// 列表点击「名称」列：名称 = site.name 字段，直接打开基础设置 Tab 修改
function editSiteName(row) {
  openSettings(row, 'base')
}

async function openLogs(row) {
  logsRef.value.open(row)
}

async function openStat(row) {
  statSiteId.value = row.id
  statSiteName.value = row.name
  statVisible.value = true
}

async function openSecurity(row) {
  securitySiteId.value = row.id
  securitySiteName.value = row.name
  securityVisible.value = true
}

async function openSSL(row) {
  sslSite.value = row
  sslVisible.value = true
}

// 默认页面抽屉
const defaultPagesRef = ref()
function openDefaultPages() {
  defaultPagesRef.value?.open()
}

async function doStateAction(row, action) {
  try {
    await request.post('/site/action', { id: row.id, action })
    const msg = action === 'start' ? '已启动' : action === 'stop' ? '已停止' : '已重启'
    ElMessage.success(msg)
    loadSites()
  } catch (e) { /* interceptor handles */ }
}

const delVisible = ref(false)
const delSubmitting = ref(false)
const delSite = ref(null)
const delOptions = reactive({ conf: true, root: true, db: true, ftp: true })

function remove(row) {
  delSite.value = row
  delOptions.root = true
  delOptions.db = true
  delOptions.ftp = true
  delVisible.value = true
}

async function confirmDelete() {
  if (!delSite.value || delSubmitting.value) return
  delSubmitting.value = true
  try {
    const res = await request.post('/site/delete', {
      id: delSite.value.id,
      del_root: delOptions.root,
      del_db: delOptions.db,
      del_ftp: delOptions.ftp
    })
    delVisible.value = false
    ElMessage.success('已删除')
    const warnings = res.data?.warnings
    if (warnings && warnings.length) {
      ElMessage.warning(warnings.join('；'))
    }
    loadSites()
  } catch (e) { /* interceptor handles */ }
  finally { delSubmitting.value = false }
}

async function loadEnvStatus() {
  try {
    const res = await request.get('/apps/env-status')
    const data = res.data || {}
    envStatusMap.value = data
      // 不再自动预勾选任何版本，避免用户一进页面就看到"一键安装(N)"按钮且
    // 可能与残留的安装状态叠加产生 loading。用户必须手动选择要安装的版本。
    // selectedVersionMap 保持用户上次手动选择，不做默认填充。
    // Web 服务器：nginx 与 apache 任一安装即视为可用
    if (data.nginx || data.apache) {
      const nginxOk = data.nginx?.installed
      const apacheOk = data.apache?.installed
      nginxInstalled.value = nginxOk || apacheOk
      // 优先记录已安装的那个；都未装时用 nginx 作为默认安装目标
      if (nginxOk) {
        nginxEnv.value = data.nginx
      } else if (apacheOk) {
        nginxEnv.value = data.apache
      } else {
        nginxEnv.value = data.nginx || data.apache || {}
      }
    }
  } catch (e) {
    nginxInstalled.value = true
  }
}

function pollNginxStatus(key) {
  if (nginxInstallTimer.value) clearInterval(nginxInstallTimer.value)
  nginxInstallTimer.value = setInterval(async () => {
    try {
      const res = await request.get('/apps/list')
      const app = (res.data || []).find((a) => a.key === key)
      if (!app) return
      const action = pendingActionMap.value.nginx
      const wsName = nginxEnv.value?.name || 'Web 服务器'
      if (action === 'install' && app.status === 'installed') {
        clearInterval(nginxInstallTimer.value)
        nginxInstallTimer.value = null
        installingNginx.value = false
        pendingActionMap.value.nginx = ''
        await loadEnvStatus()
        await loadSites()
        ElMessage.success(`${wsName} 安装完成`)
      } else if (action === 'uninstall' && app.status === 'not_installed') {
        clearInterval(nginxInstallTimer.value)
        nginxInstallTimer.value = null
        installingNginx.value = false
        pendingActionMap.value.nginx = ''
        await loadEnvStatus()
        await loadSites()
        ElMessage.success(`${wsName} 卸载完成`)
      } else if (app.status === 'failed') {
        clearInterval(nginxInstallTimer.value)
        nginxInstallTimer.value = null
        installingNginx.value = false
        pendingActionMap.value.nginx = ''
        ElMessage.error('操作失败，请查看日志')
      }
    } catch (e) {}
  }, 1000)
}

async function installNginx() {
  const meta = nginxEnv.value
  const wsName = meta?.name || 'Web 服务器'
  if (!meta || !meta.key) {
    ElMessage.warning('暂不支持一键安装 Web 服务器')
    return
  }
  installingNginx.value = true
  pendingActionMap.value.nginx = 'install'
  try {
    const payload = { key: meta.key }
    if (meta.select_version && meta.version_default) {
      payload.version = meta.version_default
    }
    await request.post('/apps/install', payload)
    const tracker = useInstallTrackerStore()
    tracker.unmarkRemoved(meta.key) // 用户主动触发：清除"被移除"标记
    tracker.upsert({ key: meta.key, name: wsName, action: 'install', status: 'queued', message: '排队等待中...' })
    ElMessage.success(`${wsName} 安装已开始，请稍候...`)
    pollNginxStatus(meta.key)
  } catch (e) {
    ElMessage.error(e?.response?.data?.message || '安装请求失败')
    installingNginx.value = false
    pendingActionMap.value.nginx = ''
  }
}

// 进入页面/刷新时探测后端是否已有进行中的 nginx 安装/卸载，避免误显示"未检测到"
async function checkPendingAction(type) {
  const meta = envStatusMap.value[type] || nginxEnv.value
  const key = meta?.key
  if (!key) return false
  try {
    const res = await request.get('/apps/list')
    const app = (res.data || []).find((a) => a.key === key)
    if (!app) return false
    if (app.status === 'installing') {
      installingNginx.value = true
      pendingActionMap.value[type] = 'install'
      pollNginxStatus(key)
      return true
    } else if (app.status === 'uninstalling') {
      installingNginx.value = true
      pendingActionMap.value[type] = 'uninstall'
      pollNginxStatus(key)
      return true
    }
  } catch (e) {}
  return false
}

async function load() {
  // 任何情况下先显示骨架屏，查询出状态后再根据状态切换
  loading.value = true
  try {
    await loadEnvStatus()
    // 先探测后端是否有进行中的 Web 服务器安装/卸载
    if (await checkPendingAction('nginx')) {
      return
    }
    await Promise.all([loadSites(), loadRuntimes()])
  } catch (e) {
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  load()
  updateResponsive()
  window.addEventListener('resize', updateResponsive)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', updateResponsive)
  if (nginxInstallTimer.value) {
    clearInterval(nginxInstallTimer.value)
    nginxInstallTimer.value = null
  }
})
</script>

<style scoped>
.card-header { display: flex; justify-content: space-between; align-items: center; }
.card-header-actions { display: flex; align-items: center; gap: 8px; }
.domain-cell { display: inline-flex; align-items: center; gap: 2px; line-height: 1; margin-left: -12px; position: relative; }
.domain-cell.has-more { padding-right: 4px; cursor: pointer; }
.domain-caret { color: #909399; cursor: pointer; font-size: 12px; margin-left: -2px; }
.domain-list-wrap { position: fixed; z-index: 2000; background: #fff; border: 1px solid #ebeef5; border-radius: 4px; box-shadow: 0 2px 12px 0 rgba(0,0,0,0.1); white-space: nowrap; padding: 4px 0; }
.domain-list { display: flex; flex-direction: column; gap: 0; padding: 0; margin: 0; max-height: 240px; overflow-y: auto; width: max-content; }
:deep(.domain-list-popover) { display: none !important; }
:deep(.domain-list-popover .el-popover__content) { display: none !important; }
.domain-list-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 5px 10px; border-radius: 0; line-height: 20px; cursor: pointer; box-sizing: border-box; }
.domain-list-row:hover { background: #f5f7fa; }
.domain-link { display: inline-flex; align-items: center; vertical-align: middle; line-height: 1; flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; text-decoration: none !important; color: #409eff; }
.domain-link:hover { text-decoration: none !important; }
.domain-text { display: block; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: #409eff; line-height: 18px; text-align: left; }
.site-tabs { margin-bottom: 4px; }
.tab-badge { margin-left: 4px; }
.env-missing {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 320px;
  background: #fafafa;
  border: 1px dashed #dcdfe6;
  border-radius: 8px;
}
/* 环境管理弹窗空状态 */
.env-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 40px 16px;
  color: #909399;
}
.env-empty p {
  margin: 12px 0 0;
  font-size: 14px;
}
.rvm-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.rvm-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  background: #fff;
  border: 1px solid #ebeef5;
  border-radius: 6px;
  transition: box-shadow 0.2s;
}
.rvm-item:hover {
  box-shadow: 0 2px 8px rgba(0,0,0,0.06);
}
.rvm-info {
  display: flex;
  align-items: center;
  gap: 10px;
}
.rvm-name {
  font-size: 14px;
  font-weight: 500;
  color: #303133;
  min-width: 100px;
}
.rvm-ver {
  font-size: 12px;
  color: #909399;
}
.rvm-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}
.env-missing-list {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 240px;
  margin-top: 16px;
  background: #fafafa;
  border: 1px dashed #dcdfe6;
  border-radius: 8px;
}
.env-missing-inner {
  text-align: center;
  padding: 32px;
}
.env-missing-inner h3 {
  margin: 12px 0 8px;
  font-size: 18px;
  color: #303133;
}
.env-missing-inner p {
  color: #606266;
  font-size: 14px;
  margin: 0 0 16px;
  max-width: 420px;
}
.version-select-row {
  display: flex;
  align-items: center;
  justify-content: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 16px;
  font-size: 14px;
  color: #303133;
}
/* 多选 select 展开所有 tag，强制多行展示，避免折叠后 X 不可点 */
.version-select-row :deep(.el-select__wrapper),
.version-select-row :deep(.el-select__tags-wrapper) {
  flex-wrap: wrap !important;
  max-height: none !important;
  height: auto !important;
  overflow: visible !important;
}
.version-select-row :deep(.el-select__tags) {
  flex-wrap: wrap;
  max-height: none;
  overflow: visible;
}
.version-select-row :deep(.el-select__tags-text) {
  display: inline-block;
  max-width: 160px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.installing-hint {
  margin-top: 12px;
  color: #409eff;
  font-size: 13px;
}

/* 删除网站弹窗 */
.del-warning {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 10px 12px;
  border-radius: 6px;
  background: #fdf6ec;
  color: #e6a23c;
  font-size: 13px;
  line-height: 1.6;
}
.del-warning b { color: #d03050; }
.del-options {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-top: 16px;
  padding: 4px 6px;
  font-size: 14px;
}
.del-options :deep(.el-checkbox__label) {
  word-break: break-all;
  white-space: normal;
}

/* 创建网站弹窗：约束高度 / 滚动 / 内边距，并始终预留滚动条空间防止闪动 */
:deep(.site-create-dialog) {
  display: flex;
  flex-direction: column;
  max-height: calc(100vh - 8vh);
}
:deep(.site-create-dialog .el-dialog__body) {
  overflow-y: auto;
  scrollbar-gutter: stable;          /* 预留滚动条占位，避免出现时内容闪移 */
  scrollbar-width: thin;           /* Firefox 窄滚动条 */
  scrollbar-color: rgba(144, 147, 153, 0.3) transparent;
  -webkit-overflow-scrolling: touch;
  flex: 1 1 auto;
  padding: 16px 20px;
}
/* Webkit 窄滚动条美化 */
:deep(.site-create-dialog .el-dialog__body::-webkit-scrollbar) {
  width: 6px;
  height: 6px;
}
:deep(.site-create-dialog .el-dialog__body::-webkit-scrollbar-track) {
  background: transparent;
}
:deep(.site-create-dialog .el-dialog__body::-webkit-scrollbar-thumb) {
  background: rgba(144, 147, 153, 0.3);
  border-radius: 3px;
}
:deep(.site-create-dialog .el-dialog__body::-webkit-scrollbar-thumb:hover) {
  background: rgba(144, 147, 153, 0.5);
}
@media (max-width: 767px) {
  :deep(.site-create-dialog) {
    margin: 0 !important;
    width: 92vw !important;
    max-width: 92vw;
  }
  :deep(.site-create-dialog .el-dialog__header) {
    padding: 14px 16px 8px;
  }
  :deep(.site-create-dialog .el-dialog__body) {
    padding: 8px 16px 16px;
    max-height: calc(100vh - 8vh - 60px);
  }
  :deep(.site-create-dialog .el-form-item__label) {
    line-height: 1.4;
    padding-bottom: 2px;
  }
  /* label 顶部对齐后，输入框占满整行 */
  :deep(.site-create-dialog .el-form-item .el-input),
  :deep(.site-create-dialog .el-form-item .el-select),
  :deep(.site-create-dialog .el-form-item .el-input-number) {
    width: 100%;
  }
}
.tip { margin-left: 8px; font-size: 12px; color: #909399; }
.tip.warn { color: #e6a23c; }

/* 创建弹窗内环境缺失提示 */
.env-alert {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 16px;
  margin-bottom: 16px;
  background: #fdf6ec;
  border: 1px solid #f5dab1;
  border-radius: 8px;
  font-size: 14px;
  color: #606266;
}
.env-alert span {
  flex: 1;
}

.remark-text {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  cursor: pointer;
  color: #606266;
  min-height: 24px;
}
.remark-text.empty {
  color: #909399;
}
.remark-text:hover {
  color: #409eff;
}
.remark-text.empty:hover {
  color: #409eff;
}
.remark-text .edit-icon {
  font-size: 12px;
  opacity: 0;
  transition: opacity 0.2s;
}
.remark-text:hover .edit-icon {
  opacity: 1;
}

.state-tag {
  cursor: pointer;
  outline: none !important;
  border-color: transparent !important;
}
.state-caret {
  margin-left: 2px;
  font-size: 12px;
  vertical-align: -2px;
}
.ssl-soon {
  color: #e6a23c;
  font-weight: 500;
}
.ssl-undeployed {
  color: #f56c6c;
}
</style>
