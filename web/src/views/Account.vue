<template>
  <div class="account-page">
    <el-card>
      <template #header><span>账号信息</span></template>
      <el-descriptions :column="1" border>
        <el-descriptions-item label="用户名">{{ user.username }}</el-descriptions-item>
        <el-descriptions-item label="昵称">{{ user.nickname || '-' }}</el-descriptions-item>
        <el-descriptions-item label="邮箱">{{ user.email || '-' }}</el-descriptions-item>
        <el-descriptions-item label="角色">{{ user.role || '-' }}</el-descriptions-item>
        <el-descriptions-item label="租户">{{ user.tenantId || '全局' }}</el-descriptions-item>
        <el-descriptions-item label="状态">
          <el-tag :type="user.status === 1 ? 'success' : 'info'" size="small">
            {{ user.status === 1 ? '正常' : '停用' }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="注册时间">{{ user.createdAt || '-' }}</el-descriptions-item>
      </el-descriptions>
    </el-card>

    <el-card style="margin-top:16px">
      <template #header><span>修改密码</span></template>
      <el-form :model="form" label-width="100px" style="max-width:480px">
        <el-form-item label="旧密码" required>
          <el-input v-model="form.oldPassword" type="password" show-password />
        </el-form-item>
        <el-form-item label="新密码" required>
          <el-input v-model="form.newPassword" type="password" show-password />
        </el-form-item>
        <el-form-item label="确认新密码" required>
          <el-input v-model="form.confirm" type="password" show-password />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="changePassword" :loading="changing">提交</el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <el-card style="margin-top:16px">
      <template #header><span>API Key（Bearer）</span></template>
      <p class="hint">当前调用 aisaas Bearer API 的 API Key（存于 localStorage，仅本地可见）。</p>
      <el-input :model-value="bearerKey" readonly>
        <template #append>
          <el-button @click="copyKey">复制</el-button>
        </template>
      </el-input>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../api'

const user = reactive({
  username: '', nickname: '', email: '', role: '', tenantId: '', status: 1, createdAt: ''
})
const form = reactive({ oldPassword: '', newPassword: '', confirm: '' })
const changing = ref(false)
const bearerKey = ref('')

async function load() {
  // 从 localStorage 取基本信息
  user.username = localStorage.getItem('username') || '-'
  user.nickname = localStorage.getItem('nickname') || ''
  bearerKey.value = localStorage.getItem('aisaas_bearer_key') || ''
  // 尝试拉完整账户信息
  try {
    const r = await api.accountMe()
    Object.assign(user, r.data || r || {})
  } catch (_) { /* 端点可能没实现，保留 localStorage 数据 */ }
}

async function changePassword() {
  if (!form.oldPassword || !form.newPassword) return ElMessage.warning('请填写完整')
  if (form.newPassword !== form.confirm) return ElMessage.error('两次输入不一致')
  if (form.newPassword.length < 8) return ElMessage.warning('新密码至少 8 位')
  changing.value = true
  try {
    await api.changePassword({ oldPassword: form.oldPassword, newPassword: form.newPassword })
    ElMessage.success('密码已修改')
    form.oldPassword = form.newPassword = form.confirm = ''
  } catch (e) { ElMessage.error(e.message || '修改失败') }
  finally { changing.value = false }
}

function copyKey() {
  navigator.clipboard?.writeText(bearerKey.value)
  ElMessage.success('已复制')
}

onMounted(load)
</script>

<style scoped>
.hint { font-size: 13px; color: #64748b; margin-bottom: 8px; }
</style>
