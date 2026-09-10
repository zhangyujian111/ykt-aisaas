<template><div class="page">
  <el-card>
    <template #header>
      <div class="toolbar">
        <el-input v-model="deviceId" placeholder="deviceId（必填）" style="width:240px" />
        <el-input v-model="kw" placeholder="按 ID / 内容过滤" style="width:200px" clearable />
        <el-button type="primary" @click="load">加载</el-button>
        <el-button @click="refresh">刷新</el-button>
      </div>
    </template>
    <el-table :data="rows" v-loading="loading" border stripe size="small">
      <el-table-column prop="id" label="ID" width="100" />
      <el-table-column prop="sessionId" label="会话" width="120" />
      <el-table-column prop="role" label="角色" width="80" />
      <el-table-column prop="content" label="内容" show-overflow-tooltip />
      <el-table-column prop="createTime" label="时间" width="180" />
    </el-table>
  </el-card>
</div></template>
<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../api'
const rows = ref([]); const kw = ref(''); const deviceId = ref('ESP32-001'); const loading = ref(false)
async function load() {
  if (!deviceId.value) { ElMessage.warning('请输入 deviceId'); return }
  loading.value = true
  try {
    const r = await api.memory(deviceId.value)
    let list = r?.data?.items || r?.data?.data?.items || []
    rows.value = kw.value ? list.filter(x => JSON.stringify(x).includes(kw.value)) : list
  } catch (e) { ElMessage.error(e.message) }
  finally { loading.value = false }
}
async function refresh() { await load() }
onMounted(load)
</script>