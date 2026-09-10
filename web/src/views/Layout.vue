<template>
  <el-container class="layout-root">
    <el-aside width="220px" class="aside">
      <div class="logo">
        <el-icon class="logo-icon"><MagicStick /></el-icon>
        <span>AI Console</span>
      </div>
      <el-menu :default-active="$route.path" router class="menu">
        <el-menu-item-group title="总览">
          <el-menu-item index="/dashboard"><el-icon><DataBoard /></el-icon><span>总览</span></el-menu-item>
          <el-menu-item index="/billing"><el-icon><Money /></el-icon><span>账单</span></el-menu-item>
        </el-menu-item-group>
        <el-menu-item-group title="AI 平台">
          <el-menu-item index="/models"><el-icon><Box /></el-icon><span>模型</span></el-menu-item>
          <el-menu-item index="/apikeys"><el-icon><Key /></el-icon><span>API Key</span></el-menu-item>
          <el-menu-item index="/personas"><el-icon><UserFilled /></el-icon><span>人设</span></el-menu-item>
          <el-menu-item index="/memory"><el-icon><Notebook /></el-icon><span>记忆</span></el-menu-item>
          <el-menu-item index="/sessions"><el-icon><Timer /></el-icon><span>会话</span></el-menu-item>
          <el-menu-item index="/mcp"><el-icon><Tools /></el-icon><span>MCP 工具</span></el-menu-item>
          <el-menu-item index="/kb"><el-icon><Document /></el-icon><span>知识库</span></el-menu-item>
          <el-menu-item index="/anomaly"><el-icon><AlarmClock /></el-icon><span>异常规则</span></el-menu-item>
        </el-menu-item-group>
        <el-menu-item-group title="租户">
          <el-menu-item index="/devices"><el-icon><Cpu /></el-icon><span>设备</span></el-menu-item>
          <el-menu-item index="/orders"><el-icon><List /></el-icon><span>订单</span></el-menu-item>
        </el-menu-item-group>
      </el-menu>
    </el-aside>
    <el-container>
      <el-header class="header">
        <div class="title">{{ $route.meta?.title || 'AI Console' }}</div>
        <div class="right">
          <el-dropdown trigger="click" @command="onCommand">
            <span class="user-chip">
              <el-avatar :size="28" class="avatar">{{ avatarText }}</el-avatar>
              <span class="name">{{ displayName }}</span>
              <el-icon><ArrowDown /></el-icon>
            </span>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item disabled>
                  <div style="font-size:12px;color:#94a3b8">@{{ username }}</div>
                </el-dropdown-item>
                <el-dropdown-item divided command="logout">
                  <el-icon><SwitchButton /></el-icon> 退出登录
                </el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
        </div>
      </el-header>
      <el-main class="main"><router-view /></el-main>
    </el-container>
  </el-container>
</template>

<script setup>
import { useRouter } from 'vue-router'
import { computed, onMounted } from 'vue'
import {
  DataBoard, Money, Box, Key, UserFilled, Notebook, Timer, Tools, Document, AlarmClock, Cpu, List,
  MagicStick, ArrowDown, SwitchButton
} from '@element-plus/icons-vue'

const router = useRouter()

const username = computed(() => localStorage.getItem('username') || '')
const nickname = computed(() => localStorage.getItem('nickname') || '')
// 显示名优先级：真实 nickname（非 ????） > username
const displayName = computed(() => {
  const n = nickname.value.trim()
  if (n && !/^\?+$/.test(n)) return n
  return username.value || '未登录'
})
const avatarText = computed(() => {
  const s = displayName.value
  return s ? s.slice(0, 1).toUpperCase() : '?'
})

function onCommand(cmd) {
  if (cmd === 'logout') {
    localStorage.removeItem('token')
    localStorage.removeItem('nickname')
    localStorage.removeItem('username')
    router.push('/login')
  }
}

onMounted(() => {
  if (!localStorage.getItem('aisaas_bearer_key')) {
    localStorage.setItem('aisaas_bearer_key', 'sk-aisaas-962101aa6a4c0af0d1b8447744c4bf12')
  }
})
</script>

<style scoped>
.layout-root { height: 100vh; }
.aside { background: #0f172a; color: #fff; overflow: auto; }
.logo { padding: 18px 16px; display: flex; align-items: center; gap: 8px; font-weight: 600; font-size: 16px; border-bottom: 1px solid #1e293b; }
.logo-icon { font-size: 20px; color: #38bdf8; }
.menu { background: #0f172a; border-right: none; }
.menu :deep(.el-menu-item-group__title) { color: #64748b; font-size: 11px; padding-left: 16px; }
.header { background: #fff; display: flex; justify-content: space-between; align-items: center; padding: 0 20px; box-shadow: 0 1px 4px rgba(15,23,42,.06); }
.title { font-size: 16px; font-weight: 600; }
.right { display: flex; align-items: center; gap: 12px; }
.user-chip { display: inline-flex; align-items: center; gap: 8px; cursor: pointer; padding: 4px 8px; border-radius: 20px; transition: background .15s; }
.user-chip:hover { background: #f1f5f9; }
.avatar { background: linear-gradient(135deg, #38bdf8, #6366f1); color: #fff; font-weight: 600; }
.name { font-size: 14px; color: #0f172a; }
.main { background: #f8fafc; padding: 16px; }
:deep(.el-menu-item) { color: #94a3b8; }
:deep(.el-menu-item.is-active) { color: #fff; background: #1e293b !important; }
:deep(.el-menu-item:hover) { background: #1e293b !important; }
</style>
