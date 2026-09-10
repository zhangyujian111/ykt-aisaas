<template><div class="page">
  <el-card><template #header><span>知识库</span></template>
    <el-table :data="rows" border stripe size="small">
      <el-table-column prop="id" label="ID" width="100" />
      <el-table-column prop="name" label="名称" />
      <el-table-column prop="docCount" label="文档数" width="120" />
      <el-table-column prop="createdAt" label="创建时间" width="180" />
    </el-table>
  </el-card>
</div></template>
<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../api'
const rows = ref([])
onMounted(async () => {
  try { rows.value = (await api.knowledgeBases()).data || (await api.knowledgeBases()) || [] } catch (e) { ElMessage.error(e.message) }
})
</script>