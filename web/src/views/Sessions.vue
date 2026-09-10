<template><div class="page">
  <el-card><template #header>
    <div class="toolbar">
      <el-input v-model="deviceId" placeholder="deviceId（必填）" style="width:240px" />
      <el-button type="primary" @click="load">加载</el-button>
      <el-button @click="refresh">刷新</el-button>
    </div>
  </template>
    <el-table :data="rows" v-loading="loading" border stripe size="small">
      <el-table-column prop="id" label="ID" width="100" />
      <el-table-column prop="deviceId" label="设备" width="160" />
      <el-table-column prop="personaId" label="人设" width="120" />
      <el-table-column prop="roundCount" label="轮次" width="80" />
      <el-table-column prop="startTime" label="开始" width="180" />
      <el-table-column prop="lastActive" label="最后活跃" width="180" />
      <el-table-column prop="status" label="状态" width="80" />
    </el-table>
  </el-card>
</div></template>
<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../api'
const rows = ref([]); const deviceId = ref('ESP32-001'); const loading = ref(false)
async function load() {
  if (!deviceId.value) { ElMessage.warning('请输入 deviceId'); return }
  loading.value = true
  try {
    const r = await api.sessions(deviceId.value)
    let list = r?.data || r || []
    if (!Array.isArray(list) && r?.data?.data) list = r.data.data
    rows.value = Array.isArray(list) ? list : []
  } catch (e) { ElMessage.error(e.message) }
  finally { loading.value = false }
}
async function refresh() { await load() }
onMounted(load)
</script>
<style scoped>.toolbar{display:flex;gap:12px;align-items:center}</style>