<template>
  <div class="ai-models">
    <el-card>
      <template #header>
        <div class="header-row">
          <div class="toolbar">
            <el-tabs v-model="activeType" @tab-change="load" class="type-tabs">
              <el-tab-pane label="全部" name="" />
              <el-tab-pane label="Chat 模型" name="chat" />
              <el-tab-pane label="ASR（语音识别）" name="asr" />
              <el-tab-pane label="TTS（语音合成）" name="tts" />
              <el-tab-pane label="Embedding" name="embedding" />
            </el-tabs>
          </div>
          <el-button type="primary" @click="openCreate">+ 新增模型</el-button>
        </div>
      </template>
      <el-table :data="rows" border stripe v-loading="loading">
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column label="类型" width="90">
          <template #default="{ row }"><el-tag size="small" :type="typeColor(row.type)">{{ row.type }}</el-tag></template>
        </el-table-column>
        <el-table-column prop="modelId" label="modelId" min-width="160" />
        <el-table-column prop="upstreamModel" label="上游模型" min-width="160" />
        <el-table-column prop="provider" label="provider" width="120" />
        <el-table-column label="baseUrl" min-width="240" show-overflow-tooltip>
          <template #default="{ row }"><code class="url">{{ row.baseUrl }}</code></template>
        </el-table-column>
        <el-table-column label="apiKey" width="160">
          <template #default="{ row }"><code class="key">{{ maskKey(row.apiKey) }}</code></template>
        </el-table-column>
        <el-table-column label="上下文" width="100">
          <template #default="{ row }">{{ row.contextLength || '-' }}</template>
        </el-table-column>
        <el-table-column label="默认" width="70">
          <template #default="{ row }"><el-tag v-if="row.isDefault" type="success" size="small">默认</el-tag></template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-switch v-model="row.status" :active-value="1" :inactive-value="0" @change="toggleStatus(row)" />
          </template>
        </el-table-column>
        <el-table-column label="操作" width="180" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="testModel(row)">测试</el-button>
            <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
            <el-button link type="danger" @click="remove(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <el-dialog v-model="dialogVisible" :title="form.id ? '编辑模型' : '新增模型'" width="640px">
      <el-form label-width="100px" :model="form">
        <el-form-item label="modelId" required><el-input v-model="form.modelId" :disabled="!!form.id" placeholder="如 qwen3.6-flash / whisper-1 / tts-1" /></el-form-item>
        <el-form-item label="provider" required><el-input v-model="form.provider" placeholder="bailian / openai / aliyun-asr / aliyun-tts" /></el-form-item>
        <el-form-item label="类型" required>
          <el-select v-model="form.type" style="width:100%">
            <el-option label="chat" value="chat" />
            <el-option label="asr" value="asr" />
            <el-option label="tts" value="tts" />
            <el-option label="embedding" value="embedding" />
          </el-select>
        </el-form-item>
        <el-form-item label="baseUrl" required><el-input v-model="form.baseUrl" placeholder="https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1" /></el-form-item>
        <el-form-item label="apiKey" :required="!form.id">
          <el-input v-model="form.apiKey" type="password" show-password placeholder="明文 API Key（AES 加密落库）" />
        </el-form-item>
        <el-form-item label="upstreamModel"><el-input v-model="form.upstreamModel" placeholder="上游真实模型名，留空默认用 modelId" /></el-form-item>
        <el-form-item label="上下文长度"><el-input-number v-model="form.contextLength" :min="0" /></el-form-item>
        <el-form-item label="流式输出"><el-switch v-model="form.isStream" /></el-form-item>
        <el-form-item label="状态"><el-switch v-model="form.status" :active-value="1" :inactive-value="0" /></el-form-item>
        <el-form-item label="默认"><el-switch v-model="form.isDefault" /></el-form-item>
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
const activeType = ref('')
const dialogVisible = ref(false)
const form = reactive({
  id: 0, modelId: '', provider: 'bailian', type: 'chat',
  baseUrl: '', apiKey: '', upstreamModel: '',
  contextLength: 32768, isStream: true, isDefault: false, status: 1
})

function typeColor(t) {
  return { chat: '', asr: 'success', tts: 'warning', embedding: 'info' }[t] || ''
}

async function load() {
  loading.value = true
  try {
    const r = await api.models(activeType.value ? { type: activeType.value } : {})
    rows.value = r.data || []
  } catch (e) { ElMessage.error(e.message || '加载失败') }
  finally { loading.value = false }
}
function maskKey(k) { if (!k) return ''; return k.length <= 14 ? k : k.slice(0,8)+'...'+k.slice(-4) }
function openCreate() {
  Object.assign(form, {
    id: 0, modelId: '', provider: 'bailian', type: activeType.value || 'chat',
    baseUrl: 'https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1',
    apiKey: '', upstreamModel: '', contextLength: 32768,
    isStream: true, isDefault: false, status: 1
  })
  dialogVisible.value = true
}
function openEdit(row) {
  Object.assign(form, { ...row, apiKey: '' })
  dialogVisible.value = true
}
async function save() {
  try {
    if (form.id) {
      const body = { ...form }; delete body.id; if (!body.apiKey) delete body.apiKey
      await api.updateModel(form.id, body)
    } else {
      await api.createModel(form)
    }
    ElMessage.success('已保存'); dialogVisible.value = false; load()
  } catch (e) { ElMessage.error(e.message || '保存失败') }
}
async function toggleStatus(row) {
  try { await api.updateModel(row.id, { status: row.status }) }
  catch (e) { ElMessage.error(e.message || '更新失败'); row.status = row.status === 1 ? 0 : 1 }
}
async function remove(row) {
  try { await ElMessageBox.confirm(`确认删除 ${row.modelId}?`) } catch { return }
  try { await api.deleteModel(row.id); ElMessage.success('已删除'); load() }
  catch (e) { ElMessage.error(e.message) }
}
async function testModel(row) {
  if (!row.apiKey) return ElMessage.warning('该模型未配置 apiKey，无法测试')
  try {
    const r = await api.testModel({
      baseUrl: row.baseUrl, apiKey: row.apiKey,
      upstreamModel: row.upstreamModel || row.modelId, type: row.type
    })
    if (r.data?.status === 200 || r.data?.code === 0) ElMessage.success('连接成功')
    else ElMessage.warning(`上游返回 ${JSON.stringify(r.data).slice(0,200)}`)
  } catch (e) { ElMessage.error('测试失败：' + (e.message || '')) }
}
onMounted(load)
</script>

<style scoped>
.header-row { display:flex; justify-content:space-between; align-items:flex-start; gap:12px; }
.type-tabs { margin-bottom: -8px; }
.toolbar { flex:1; }
.url { font-family:monospace; font-size:12px; color:#475569; }
.key { font-family:monospace; font-size:12px; color:#64748b; }
</style>
