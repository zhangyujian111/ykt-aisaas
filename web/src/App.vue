<template>
  <el-container style="min-height: 100vh">
    <el-aside v-if="logged" width="200px" style="border-right: 1px solid #eee; padding: 16px">
      <h3 style="margin: 0 0 24px">🤖 AI 控制台</h3>
      <el-menu router :default-active="$route.path">
        <el-menu-item index="/devices">我的设备</el-menu-item>
        <el-menu-item index="/orders">充值记录</el-menu-item>
      </el-menu>
      <div style="margin-top: 40px; font-size: 12px; color: #999">
        {{ nickname }}<br />
        <el-button link type="danger" @click="logout">退出登录</el-button>
      </div>
    </el-aside>
    <el-main style="background: #f7f8fa">
      <router-view />
    </el-main>
  </el-container>
</template>
<script setup>
import { computed } from 'vue'
import { useRouter } from 'vue-router'
const router = useRouter()
const logged = computed(() => !!localStorage.getItem('token'))
const nickname = computed(() => localStorage.getItem('nickname') || '')
function logout() {
  localStorage.clear()
  router.push('/login')
}
</script>
