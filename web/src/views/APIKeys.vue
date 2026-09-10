<template><div class="page">
  <el-card><template #header><span>API Key（按租户）</span></template>
    <el-row :gutter="12">
      <el-col :span="6"><el-input v-model.number="tenantId" placeholder="租户 ID" /></el-col>
      <el-col :span="4"><el-button type="primary" @click="load">加载</el-button></el-col>
    </el-row>
    <el-table :data="keys" border size="small" style="margin-top:12px">
      <el-table-column prop="id" label="ID" width="100" />
      <el-table-column prop="plain" label="密钥" />
      <el-table-column prop="status" label="状态" width="100">
        <template #default="{ row }"><el-tag :type="row.status === 1 ? 'success' : 'info'">{{ row.status === 1 ? '启用' : '停用' }}</el-tag></template>
      </el-table-column>
      <el-table-column prop="createdAt" label="创建时间" width="180" />
      <el-table-column label="操作" width="160">
        <template #default="{ row }">
          <el-button link type="warning" @click="rotate(row)">轮换</el-button>
          <el-button link type="danger" @click="revoke(row)">撤销</el-button>
        </template>
      </el-table-column>
    </el-table>
  </el-card>
</div></template>
<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../api'
const keys = ref([]); const tenantId = ref(1)
async function load() { try { keys.value = await api.apikeys(tenantId.value) } catch (e) { ElMessage.error(e.message) } }
async function rotate(row) { try { await ElMessageBox.confirm(`轮换 key ${row.id}?`) } catch { return }; ElMessage.success('已轮换（请实现 rotate 接口）') }
async function revoke(row) { try { await ElMessageBox.confirm(`撤销 key ${row.id}?`) } catch { return }; ElMessage.success('已撤销（请实现 revoke 接口）') }
onMounted(load)
</script>