<template>
  <div class="tpl-page">
    <el-card>
      <template #header>
        <div class="header-row">
          <div class="toolbar">
            <el-select v-model="categoryFilter" placeholder="类型" clearable style="width:180px" @change="load">
              <el-option label="全部" value="" />
              <el-option label="记忆摘要" value="memory_summary" />
              <el-option label="人设" value="persona" />
              <el-option label="系统提示" value="system" />
              <el-option label="自定义" value="custom" />
            </el-select>
          </div>
          <el-button type="primary" @click="openCreate">+ 新增模板</el-button>
        </div>
      </template>
      <el-table :data="rows" border stripe v-loading="loading">
        <el-table-column prop="id" label="ID" width="70" />
        <el-table-column prop="category" label="类型" width="120">
          <template #default="{ row }">
            <el-tag size="small" :type="catColor(row.category)">{{ catLabel(row.category) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="templateKey" label="key" min-width="160" />
        <el-table-column prop="name" label="名称" min-width="160" />
        <el-table-column prop="description" label="说明" show-overflow-tooltip />
        <el-table-column label="默认" width="70">
          <template #default="{ row }">
            <el-tag v-if="row.isDefault" type="success" size="small">默认</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="80">
          <template #default="{ row }">
            <el-switch v-model="row.status" :active-value="1" :inactive-value="0" @change="toggleStatus(row)" />
          </template>
        </el-table-column>
        <el-table-column label="操作" width="180" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
            <el-button link type="danger" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="dialogVisible" :title="form.id ? '编辑模板' : '新增模板'" width="720px">
      <el-form :model="form" label-width="100px">
        <el-form-item label="key" required>
          <el-input v-model="form.templateKey" :disabled="!!form.id" placeholder="如 memory_summary_v1 / persona_default" />
        </el-form-item>
        <el-form-item label="名称" required>
          <el-input v-model="form.name" />
        </el-form-item>
        <el-form-item label="类型" required>
          <el-select v-model="form.category" style="width:100%">
            <el-option label="记忆摘要" value="memory_summary" />
            <el-option label="人设" value="persona" />
            <el-option label="系统提示" value="system" />
            <el-option label="自定义" value="custom" />
          </el-select>
        </el-form-item>
        <el-form-item label="说明">
          <el-input v-model="form.description" />
        </el-form-item>
        <el-form-item label="变量定义">
          <el-input v-model="form.variables" type="textarea" :rows="2"
            placeholder='JSON 数组：["userMessage","history","personaName"]' />
        </el-form-item>
        <el-form-item label="内容" required>
          <el-input v-model="form.content" type="textarea" :rows="10"
            placeholder="支持 Go template 语法：{{.UserMessage}} {{.PersonaName}}" />
        </el-form-item>
        <el-form-item label="默认">
          <el-switch v-model="form.isDefault" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible=false">取消</el-button>
        <el-button type="primary" @click="save">保存</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import api from '../api'

const rows = ref([])
const loading = ref(false)
const categoryFilter = ref('')
const dialogVisible = ref(false)
const form = reactive({
  id: 0, templateKey: '', name: '', category: 'custom',
  description: '', content: '', variables: '',
  isDefault: false, status: 1
})

function catColor(c) {
  return { memory_summary: 'warning', persona: 'success', system: 'danger', custom: '' }[c] || ''
}
function catLabel(c) {
  return { memory_summary: '记忆摘要', persona: '人设', system: '系统提示', custom: '自定义' }[c] || c
}

async function load() {
  loading.value = true
  try {
    const r = await api.templates(categoryFilter.value ? { category: categoryFilter.value } : {})
    rows.value = r.data || []
  } catch (e) { ElMessage.error(e.message) }
  finally { loading.value = false }
}
function openCreate() {
  Object.assign(form, {
    id: 0, templateKey: '', name: '', category: 'custom',
    description: '', content: '', variables: '',
    isDefault: false, status: 1
  })
  dialogVisible.value = true
}
function openEdit(row) {
  Object.assign(form, row)
  dialogVisible.value = true
}
async function save() {
  try {
    if (form.id) await api.updateTemplate(form.id, form)
    else await api.createTemplate(form)
    ElMessage.success('已保存'); dialogVisible.value = false; load()
  } catch (e) { ElMessage.error(e.message) }
}
async function toggleStatus(row) {
  try { await api.updateTemplate(row.id, { status: row.status }) }
  catch (e) { ElMessage.error(e.message); row.status = row.status === 1 ? 0 : 1 }
}
async function remove(row) {
  try { await ElMessageBox.confirm(`确认删除模板「${row.name}」？`) } catch { return }
  try { await api.deleteTemplate(row.id); ElMessage.success('已删除'); load() }
  catch (e) { ElMessage.error(e.message) }
}
onMounted(load)
</script>

<style scoped>
.header-row { display:flex; justify-content:space-between; align-items:center; gap:12px; }
.toolbar { display:flex; gap:8px; align-items:center; }
</style>
