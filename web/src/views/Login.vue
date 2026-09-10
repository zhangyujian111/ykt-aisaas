<template>
  <div style="display: flex; justify-content: center; padding-top: 10vh">
    <el-card style="width: 380px">
      <h2 style="margin-top: 0">YKT AI 控制台</h2>
      <el-tabs v-model="tab">
        <el-tab-pane label="登录" name="login">
          <el-form @submit.prevent>
            <el-form-item><el-input v-model="f.username" placeholder="用户名" /></el-form-item>
            <el-form-item><el-input v-model="f.password" type="password" placeholder="密码" show-password /></el-form-item>
            <el-button type="primary" style="width: 100%" :loading="loading" @click="doLogin">登录</el-button>
          </el-form>
        </el-tab-pane>
        <el-tab-pane label="注册" name="reg">
          <el-form @submit.prevent>
            <el-form-item><el-input v-model="f.username" placeholder="用户名（≥3位）" /></el-form-item>
            <el-form-item><el-input v-model="f.password" type="password" placeholder="密码（≥6位）" show-password /></el-form-item>
            <el-form-item><el-input v-model="f.nickname" placeholder="昵称（可选）" /></el-form-item>
            <el-button type="success" style="width: 100%" :loading="loading" @click="doRegister">注册并登录</el-button>
          </el-form>
        </el-tab-pane>
      </el-tabs>
    </el-card>
  </div>
</template>
<script setup>
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import api from '../api'

const tab = ref('login')
const loading = ref(false)
const f = reactive({ username: '', password: '', nickname: '' })
const router = useRouter()

async function doLogin() {
  loading.value = true
  try {
    const d = await api.login({ username: f.username, password: f.password })
    localStorage.setItem('token', d.accessToken)
    localStorage.setItem('username', d.user.username)
    const nick = d.user.nickname || d.user.username
    // 后端 nickname 字段若是占位 '????' 则用 username
    localStorage.setItem('nickname', /^\?+$/.test(nick) ? d.user.username : nick)
    // 同步设置默认 dev API Key（/api/v1/* Bearer 鉴权用）
    if (!localStorage.getItem('aisaas_bearer_key')) {
      localStorage.setItem('aisaas_bearer_key', 'sk-aisaas-962101aa6a4c0af0d1b8447744c4bf12')
    }
    router.push('/dashboard')
  } catch (e) { ElMessage.error(e.message || '登录失败') } finally { loading.value = false }
}
async function doRegister() {
  loading.value = true
  try {
    await api.register(f)
    await doLogin()
  } catch (e) { ElMessage.error(e.message || '注册失败') } finally { loading.value = false }
}
</script>
