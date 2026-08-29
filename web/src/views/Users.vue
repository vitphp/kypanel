<template>
  <div class="users-page">
    <el-tabs v-model="activeTab">
      <!-- 用户列表 -->
      <el-tab-pane label="用户列表" name="users">
        <el-card shadow="never">
          <template #header>
            <div class="card-head">
              <span>子账号管理</span>
              <el-button type="primary" size="small" @click="createVisible = true">新建子账号</el-button>
            </div>
          </template>
          <Skeleton v-if="usersLoading" type="table" :rows="6" :columns="[{width:'60px'},{width:'160px'},{width:'140px'},{width:'80px'},{flex:1}]" />
          <el-table v-else :data="users" size="small">
            <el-table-column prop="id" label="ID" width="60" />
            <el-table-column prop="username" label="账号" width="160" />
            <el-table-column prop="role_name" label="角色" width="140">
              <template #default="{ row }">
                <el-tag :type="row.role_id === 0 ? 'danger' : 'primary'" size="small">{{ row.role_name }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="2FA" width="80">
              <template #default="{ row }">
                <el-tag :type="row.totp_enabled ? 'success' : 'info'" size="small">{{ row.totp_enabled ? '已启用' : '未启用' }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="操作" min-width="220">
              <template #default="{ row }">
                <div class="ops-cell">
                  <template v-if="row.role_id !== 0">
                    <el-button link type="primary" size="small" @click="editRole(row)">改角色</el-button>
                    <el-button link type="warning" size="small" @click="resetPwd(row)">重置密码</el-button>
                    <el-button link type="danger" size="small" @click="removeUser(row)">删除</el-button>
                  </template>
                  <span v-else class="super-tip">超级管理员</span>
                </div>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-tab-pane>

      <!-- 角色管理 -->
      <el-tab-pane label="角色管理" name="roles">
        <el-card shadow="never">
          <template #header>
            <div class="card-head">
              <span>角色管理</span>
              <el-button type="primary" size="small" @click="addRole">新建角色</el-button>
            </div>
          </template>
          <Skeleton v-if="rolesLoading" type="table" :rows="5" :columns="[{width:'60px'},{width:'140px'},{flex:1},{flex:1},{width:'120px'}]" />
          <el-table v-else :data="roles" size="small">
            <el-table-column prop="id" label="ID" width="60" />
            <el-table-column prop="name" label="角色名" width="140">
              <template #default="{ row }">
                {{ row.name }}<el-tag v-if="row.builtin" type="info" size="small" style="margin-left: 6px">内置</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="remark" label="备注" show-overflow-tooltip />
            <el-table-column label="权限" show-overflow-tooltip>
              <template #default="{ row }">{{ permText(row.permissions) }}</template>
            </el-table-column>
            <el-table-column label="操作" width="120">
              <template #default="{ row }">
                <div class="ops-cell">
                  <el-button link type="primary" size="small" @click="editRolePerm(row)">编辑</el-button>
                  <el-button v-if="!row.builtin" link type="danger" size="small" @click="removeRole(row)">删除</el-button>
                </div>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-tab-pane>
    </el-tabs>

    <!-- 新建子账号对话框 -->
    <el-dialog v-model="createVisible" title="新建子账号" width="420px">
      <el-form label-width="90px">
        <el-form-item label="账号" required><el-input v-model="createForm.username" placeholder="登录账号" /></el-form-item>
        <el-form-item label="密码" required><el-input v-model="createForm.password" type="password" show-password placeholder="至少 6 位" /></el-form-item>
        <el-form-item label="角色">
          <el-select v-model="createForm.role_id" placeholder="选择角色" style="width: 100%">
            <el-option v-for="r in roles" :key="r.id" :label="r.name" :value="r.id" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="confirmCreate">创建</el-button>
      </template>
    </el-dialog>

    <!-- 角色编辑对话框 -->
    <el-dialog v-model="roleVisible" :title="roleForm.id ? '编辑角色' : '新建角色'" width="480px">
      <el-form label-width="90px">
        <el-form-item label="角色名" required><el-input v-model="roleForm.name" :disabled="roleForm.builtin" placeholder="角色名称" /></el-form-item>
        <el-form-item label="备注"><el-input v-model="roleForm.remark" placeholder="角色说明（可选）" /></el-form-item>
        <el-form-item label="权限">
          <div class="perm-list">
            <el-checkbox v-model="allPerm" @change="toggleAll">全部权限</el-checkbox>
            <el-divider style="margin: 8px 0" />
            <el-checkbox-group v-model="roleForm.permList">
              <el-checkbox v-for="m in modules" :key="m.Key" :value="m.Key" :disabled="roleForm.builtin">{{ m.Label }}</el-checkbox>
            </el-checkbox-group>
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="roleVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="confirmRole">保存</el-button>
      </template>
    </el-dialog>

    <!-- 改角色 / 重置密码对话框 -->
    <el-dialog v-model="userEditVisible" title="修改子账号" width="380px">
      <el-form label-width="90px">
        <el-form-item v-if="userEditMode === 'role'" label="角色">
          <el-select v-model="userEditRoleId" style="width: 100%">
            <el-option v-for="r in roles" :key="r.id" :label="r.name" :value="r.id" />
          </el-select>
        </el-form-item>
        <el-form-item v-else label="新密码">
          <el-input v-model="userEditPwd" type="password" show-password placeholder="至少 6 位" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="userEditVisible = false">取消</el-button>
        <el-button type="primary" :loading="saving" @click="confirmUserEdit">确定</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import request from '../utils/request'

const activeTab = ref('users')
const users = ref([])
const usersLoading = ref(true)
const roles = ref([])
const rolesLoading = ref(true)
const modules = ref([])
const createVisible = ref(false)
const saving = ref(false)
const createForm = ref({ username: '', password: '', role_id: null })

const roleVisible = ref(false)
const roleForm = ref({ id: 0, name: '', remark: '', permissions: '', permList: [], builtin: false })

const userEditVisible = ref(false)
const userEditMode = ref('role')
const userEditId = ref(0)
const userEditRoleId = ref(null)
const userEditPwd = ref('')

const allPerm = computed({
  get: () => roleForm.value.permList.includes('*'),
  set: () => {}
})

async function load() {
  try {
    const [u, r, m] = await Promise.all([
      request.get('/users'),
      request.get('/roles'),
      request.get('/roles/modules')
    ])
    users.value = u.data || []
    roles.value = r.data || []
    modules.value = m.data || []
  } finally {
    usersLoading.value = false
    rolesLoading.value = false
  }
}

function permText(perms) {
  if (perms === '*') return '全部权限'
  if (!perms) return '无权限'
  const names = perms.split(',').map(k => {
    const m = modules.value.find(x => x.Key === k)
    return m ? m.Label : k
  })
  return names.join('、')
}

async function confirmCreate() {
  if (!createForm.value.username.trim()) return ElMessage.warning('请填写账号')
  if (createForm.value.password.length < 6) return ElMessage.warning('密码至少 6 位')
  saving.value = true
  try {
    await request.post('/users', createForm.value)
    ElMessage.success('子账号已创建')
    createVisible.value = false
    createForm.value = { username: '', password: '', role_id: null }
    load()
  } catch (e) { /* interceptor */ } finally { saving.value = false }
}

function addRole() {
  roleForm.value = { id: 0, name: '', remark: '', permissions: '', permList: [], builtin: false }
  roleVisible.value = true
}

function editRolePerm(row) {
  roleForm.value = {
    id: row.id,
    name: row.name,
    remark: row.remark,
    permissions: row.permissions,
    permList: row.permissions === '*' ? ['*'] : (row.permissions ? row.permissions.split(',') : []),
    builtin: row.builtin
  }
  roleVisible.value = true
}

function toggleAll(val) {
  if (val) roleForm.value.permList = ['*']
  else roleForm.value.permList = []
}

async function confirmRole() {
  if (!roleForm.value.name.trim()) return ElMessage.warning('请填写角色名')
  const perms = roleForm.value.permList.includes('*') ? '*' : roleForm.value.permList.join(',')
  saving.value = true
  try {
    await request.post('/roles', {
      id: roleForm.value.id,
      name: roleForm.value.name.trim(),
      remark: roleForm.value.remark,
      permissions: perms
    })
    ElMessage.success('角色已保存')
    roleVisible.value = false
    load()
  } catch (e) { /* interceptor */ } finally { saving.value = false }
}

async function removeRole(row) {
  try {
    await ElMessageBox.confirm(`确定删除角色「${row.name}」吗？`, '删除角色', { type: 'warning' })
  } catch { return }
  await request.post('/roles/delete', { id: row.id })
  ElMessage.success('已删除')
  load()
}

function editRole(row) {
  userEditMode.value = 'role'
  userEditId.value = row.id
  userEditRoleId.value = row.role_id
  userEditVisible.value = true
}

function resetPwd(row) {
  userEditMode.value = 'pwd'
  userEditId.value = row.id
  userEditPwd.value = ''
  userEditVisible.value = true
}

async function confirmUserEdit() {
  saving.value = true
  try {
    if (userEditMode.value === 'role') {
      await request.post('/users/role', { id: userEditId.value, role_id: userEditRoleId.value })
    } else {
      if (userEditPwd.value.length < 6) { saving.value = false; return ElMessage.warning('密码至少 6 位') }
      await request.post('/users/password', { id: userEditId.value, password: userEditPwd.value })
    }
    ElMessage.success('已更新')
    userEditVisible.value = false
    load()
  } catch (e) { /* interceptor */ } finally { saving.value = false }
}

async function removeUser(row) {
  try {
    await ElMessageBox.confirm(`确定删除子账号「${row.username}」吗？`, '删除子账号', { type: 'warning' })
  } catch { return }
  await request.post('/users/delete', { id: row.id })
  ElMessage.success('已删除')
  load()
}

onMounted(load)
</script>

<style scoped>
.card-head { display: flex; justify-content: space-between; align-items: center; }
.ops-cell { display: inline-flex; flex-wrap: wrap; gap: 0; }
.super-tip { color: #c0c4cc; font-size: 12px; }
.perm-list { width: 100%; }
.perm-list .el-checkbox { margin-right: 16px; margin-bottom: 6px; }
</style>
