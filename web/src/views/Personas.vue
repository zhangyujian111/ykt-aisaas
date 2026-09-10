<template><div class="page">
  <el-card>
    <template #header>
      <div class="toolbar">
        <el-input v-model="deviceId" placeholder="deviceId（必填，例 ESP32-001）" style="width:240px" />
        <el-input v-model="kw" placeholder="按 code / name 过滤" style="width:200px" clearable />
        <el-button type="primary" @click="load">加载</el-button>
        <el-button @click="openCreate">+ 新增人设</el-button>
      </div>
    </template>
    <el-table :data="rows" border stripe size="small" v-loading="loading">
      <el-table-column prop="code" label="代码" width="160" />
      <el-table-column prop="name" label="名称" width="200" />
      <el-table-column prop="defaultModelId" label="默认模型" width="160" />
      <el-table-column prop="voicePreference" label="语音" width="100" />
      <el-table-column prop="temperature" label="温度" width="80" />
      <el-table-column prop="maxTokens" label="tokens" width="100" />
      <el-table-column label="系统提示词" show-overflow-tooltip prop="systemPrompt" />
      <el-table-column label="操作" width="160">
        <template #default="{ row }">
          <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
          <el-button link type="danger" @click="remove(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>
  </el-card>

  <el-dialog v-model="dialogVisible" :title="form.id ? '编辑人设' : '新增人设'" width="640px">
    <el-form label-width="120px">
      <el-form-item label="代码"><el-input v-model="form.code" :disabled="!!form.id" /></el-form-item>
      <el-form-item label="名称"><el-input v-model="form.name" /></el-form-item>
      <el-form-item label="默认模型"><el-input v-model="form.defaultModelId" /></el-form-item>
      <el-form-item label="语音偏好"><el-input v-model="form.voicePreference" /></el-form-item>
      <el-form-item label="温度"><el-input-number v-model="form.temperature" :step="0.05" :min="0" :max="2" /></el-form-item>
      <el-form-item label="topP"><el-input-number v-model="form.topP" :step="0.05" :min="0" :max="1" /></el-form-item>
      <el-form-item label="maxTokens"><el-input-number v-model="form.maxTokens" /></el-form-item>
      <el-form-item label="系统提示词"><el-input v-model="form.systemPrompt" type="textarea" :rows="6" /></el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="dialogVisible=false">取消</el-button>
      <el-button type="primary" @click="save">保存</el-button>
    </template>
  </el-dialog>
</div></template>
<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../api'
const rows = ref([]); const kw = ref(''); const deviceId = ref('ESP32-001'); const loading = ref(false)
const dialogVisible = ref(false); const form = reactive({})
function reset() { Object.assign(form, { id: 0, code: '', name: '', defaultModelId: 'qwen3.6-plus', voicePreference: 'alloy', temperature: 0.7, topP: 0.9, maxTokens: 4096, systemPrompt: '' }) }
async function load() {
  if (!deviceId.value) { ElMessage.warning('请输入 deviceId'); return }
  loading.value = true
  try {
    const r = await api.personas(deviceId.value)
    // 后端返回 {items: [...]}，axios 拦截器已解一层 data → r.data = {items: [...]}
    let list = r?.data?.items || r?.data?.data?.items || []
    rows.value = kw.value ? list.filter(p => (p.code || '').includes(kw.value) || (p.name || '').includes(kw.value)) : list
  } catch (e) { ElMessage.error(e.message) }
  finally { loading.value = false }
}
function openCreate() { reset(); dialogVisible.value = true }
function openEdit(row) { Object.assign(form, row); dialogVisible.value = true }
async function save() {
  try {
    if (form.id) await api.updatePersona(form.id, form)
    else await api.createPersona(form)
    ElMessage.success('已保存'); dialogVisible.value = false; load()
  } catch (e) { ElMessage.error(e.message) }
}
async function remove(row) {
  try { await ElMessageBox.confirm(`删除 ${row.name}?`) } catch { return }
  try { await api.deletePersona(row.id); ElMessage.success('已删除'); load() } catch (e) { ElMessage.error(e.message) }
}
onMounted(load)
</script>
<style scoped>.toolbar{display:flex;gap:12px;align-items:center}</style>