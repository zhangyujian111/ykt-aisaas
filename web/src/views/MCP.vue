<template><div class="page">
  <el-card><template #header><span>MCP 工具</span></template>
    <el-table :data="rows" border stripe size="small">
      <el-table-column prop="name" label="名称" />
      <el-table-column prop="description" label="描述" show-overflow-tooltip />
      <el-table-column prop="enabled" label="状态" width="100">
        <template #default="{ row }"><el-tag :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? '启用' : '停用' }}</el-tag></template>
      </el-table-column>
    </el-table>
  </el-card>
</div></template>
<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../api'
const rows = ref([])
onMounted(async () => {
  try { rows.value = (await api.mcpTools()).data || (await api.mcpTools()) || [] } catch (e) { ElMessage.error(e.message) }
})
</script>