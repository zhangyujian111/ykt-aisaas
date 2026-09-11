<template>
  <div class="kb-page">
    <el-row :gutter="16">
      <el-col :span="activeKb ? 8 : 24">
        <el-card>
          <template #header>
            <div class="header-row">
              <span>知识库</span>
              <el-button type="primary" size="small" @click="openCreate">+ 新建</el-button>
            </div>
          </template>
          <el-table :data="kbs" highlight-current-row @row-click="selectKb" v-loading="loading">
            <el-table-column prop="id" label="ID" width="60" />
            <el-table-column prop="name" label="名称" />
            <el-table-column prop="docCount" label="文档" width="80" />
            <el-table-column label="操作" width="80">
              <template #default="{ row }">
                <el-button link type="danger" size="small" @click.stop="removeKb(row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>

      <el-col v-if="activeKb" :span="16">
        <el-card>
          <template #header>
            <el-tabs v-model="activeTab" class="kb-tabs">
              <el-tab-pane label="文档" name="docs">
                <template #label>
                  <span><el-icon><Document /></el-icon> 文档（{{ docs.length }}）</span>
                </template>
              </el-tab-pane>
              <el-tab-pane label="检索测试" name="search">
                <template #label>
                  <span><el-icon><Search /></el-icon> 检索测试</span>
                </template>
              </el-tab-pane>
            </el-tabs>
          </template>

          <div v-if="activeTab === 'docs'">
            <div class="upload-row">
              <el-upload :show-file-list="false" :before-upload="onPickFile" accept=".txt,.md,.pdf,.docx">
                <el-button type="primary" :loading="uploading">
                  <el-icon><Upload /></el-icon> 上传文档
                </el-button>
              </el-upload>
              <span class="hint">支持 .txt / .md / .pdf / .docx（≤50MB）</span>
            </div>
            <el-table :data="docs" v-loading="loadingDocs">
              <el-table-column prop="id" label="ID" width="60" />
              <el-table-column prop="fileName" label="文件名" />
              <el-table-column prop="fileSize" label="大小" width="100">
                <template #default="{ row }">{{ formatSize(row.fileSize) }}</template>
              </el-table-column>
              <el-table-column label="状态" width="100">
                <template #default="{ row }">
                  <el-tag :type="statusType(row.status)" size="small">{{ statusLabel(row.status) }}</el-tag>
                </template>
              </el-table-column>
              <el-table-column prop="chunkCount" label="切片数" width="80" />
            </el-table>
          </div>

          <div v-if="activeTab === 'search'">
            <div class="search-row">
              <el-input v-model="queryText" placeholder="输入问题检索相关切片" clearable @keyup.enter="doSearch" style="flex:1" />
              <el-button type="primary" @click="doSearch" :loading="searching">检索</el-button>
            </div>
            <el-table :data="results" v-loading="searching" empty-text="输入问题后点击检索">
              <el-table-column label="排名" width="60">
                <template #default="{ $index }">{{ $index + 1 }}</template>
              </el-table-column>
              <el-table-column label="内容" min-width="400">
                <template #default="{ row }">
                  <div class="snippet">{{ row.text || row.content }}</div>
                </template>
              </el-table-column>
              <el-table-column label="相关度" width="100">
                <template #default="{ row }">
                  <el-tag size="small">{{ (row.score || 0).toFixed(3) }}</el-tag>
                </template>
              </el-table-column>
            </el-table>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-dialog v-model="createVisible" title="新建知识库" width="480px">
      <el-form :model="createForm" label-width="100px">
        <el-form-item label="名称" required>
          <el-input v-model="createForm.name" placeholder="如 产品手册-2026" />
        </el-form-item>
        <el-form-item label="Embedding 模型">
          <el-input v-model="createForm.embeddingModel" placeholder="text-embedding-v3" />
        </el-form-item>
        <el-form-item label="切片大小">
          <el-input-number v-model="createForm.chunkSize" :min="100" :max="4000" :step="100" />
        </el-form-item>
        <el-form-item label="切片重叠">
          <el-input-number v-model="createForm.chunkOverlap" :min="0" :max="500" :step="50" />
        </el-form-item>
        <el-form-item label="说明">
          <el-input v-model="createForm.description" type="textarea" :rows="2" />
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="createVisible=false">取消</el-button>
        <el-button type="primary" :loading="creating" @click="doCreate">创建</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Document, Search, Upload } from '@element-plus/icons-vue'
import api from '../api'

const kbs = ref([])
const loading = ref(false)
const activeKb = ref(null)
const activeTab = ref('docs')

const docs = ref([])
const loadingDocs = ref(false)
const uploading = ref(false)

const queryText = ref('')
const searching = ref(false)
const results = ref([])

const createVisible = ref(false)
const creating = ref(false)
const createForm = reactive({
  name: '', description: '', embeddingModel: 'text-embedding-v3',
  chunkSize: 800, chunkOverlap: 100
})

async function load() {
  loading.value = true
  try { kbs.value = (await api.knowledgeBases()) || [] }
  catch (e) { ElMessage.error(e.message || '加载失败') }
  finally { loading.value = false }
}

function selectKb(row) {
  activeKb.value = row
  activeTab.value = 'docs'
  loadDocs()
}

async function loadDocs() {
  if (!activeKb.value) return
  loadingDocs.value = true
  try { docs.value = (await api.kbDocs(activeKb.value.id)) || [] }
  catch (e) { ElMessage.error(e.message) }
  finally { loadingDocs.value = false }
}

function openCreate() {
  Object.assign(createForm, {
    name: '', description: '', embeddingModel: 'text-embedding-v3',
    chunkSize: 800, chunkOverlap: 100
  })
  createVisible.value = true
}

async function doCreate() {
  if (!createForm.name) return ElMessage.warning('请输入名称')
  creating.value = true
  try {
    await api.createKb(createForm)
    ElMessage.success('已创建'); createVisible.value = false; load()
  } catch (e) { ElMessage.error(e.message) }
  finally { creating.value = false }
}

async function onPickFile(file) {
  if (!activeKb.value) {
    ElMessage.warning('请先选择知识库'); return false
  }
  uploading.value = true
  const fd = new FormData()
  fd.append('file', file)
  try {
    await api.uploadDoc(activeKb.value.id, fd)
    ElMessage.success('已上传，正在处理')
    setTimeout(loadDocs, 500)
  } catch (e) { ElMessage.error(e.message) }
  finally { uploading.value = false }
  return false
}

async function removeKb(row) {
  try { await ElMessageBox.confirm(`确认删除知识库「${row.name}」及其所有文档？`) } catch { return }
  try { await api.deleteKb(row.id); ElMessage.success('已删除'); activeKb.value = null; load() }
  catch (e) { ElMessage.error(e.message) }
}

async function doSearch() {
  if (!activeKb.value || !queryText.value.trim()) return
  searching.value = true
  try {
    const r = await api.kbSearch(activeKb.value.id, queryText.value.trim(), 5)
    results.value = r || []
    if (!results.value.length) ElMessage.info('未命中相关切片')
  } catch (e) { ElMessage.error(e.message) }
  finally { searching.value = false }
}

function formatSize(b) {
  if (!b) return '0B'
  if (b < 1024) return b + 'B'
  if (b < 1024*1024) return (b/1024).toFixed(1) + 'KB'
  return (b/1024/1024).toFixed(2) + 'MB'
}
function statusLabel(s) {
  return { 0: '待处理', 1: '处理中', 2: '就绪', 3: '失败' }[s] || '未知'
}
function statusType(s) {
  return { 0: 'info', 1: 'warning', 2: 'success', 3: 'danger' }[s] || ''
}

onMounted(load)
</script>

<style scoped>
.header-row { display:flex; justify-content:space-between; align-items:center; }
.kb-tabs { margin-bottom: -8px; }
.upload-row { display:flex; gap:12px; align-items:center; margin-bottom: 12px; }
.hint { font-size:12px; color:#94a3b8; }
.search-row { display:flex; gap:8px; margin-bottom: 12px; }
.snippet {
  white-space: pre-wrap;
  word-break: break-word;
  font-size: 13px;
  line-height: 1.6;
  color: #334155;
  background: #f8fafc;
  padding: 8px;
  border-radius: 4px;
  border-left: 3px solid #38bdf8;
}
</style>
