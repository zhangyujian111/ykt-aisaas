<template>
  <div class="memory-page">
    <el-card>
      <template #header>
        <div class="header-row">
          <div class="toolbar">
            <el-input v-model="deviceId" placeholder="deviceId（必填）" style="width:200px" />
            <el-input v-model="kw" placeholder="按内容过滤" style="width:200px" clearable />
            <el-button type="primary" @click="load">加载</el-button>
          </div>
          <div class="actions">
            <el-button type="success" :loading="extracting" @click="doExtract" :disabled="!deviceId">
              <el-icon><MagicStick /></el-icon> 提取记忆
            </el-button>
            <el-button type="warning" :loading="summarizing" @click="doSummarize" :disabled="!deviceId">
              <el-icon><Document /></el-icon> 摘要记忆
            </el-button>
          </div>
        </div>
      </template>

      <el-tabs v-model="activeTab" class="memory-tabs">
        <el-tab-pane label="短期对话（chat）" name="chat">
          <el-table :data="chatRows" v-loading="loading" border stripe size="small" empty-text="暂无对话">
            <el-table-column prop="id" label="ID" width="80" />
            <el-table-column prop="sessionId" label="会话" width="160" />
            <el-table-column prop="role" label="角色" width="100">
              <template #default="{ row }">
                <el-tag size="small" :type="row.role === 'user' ? 'primary' : 'success'">{{ row.role }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="content" label="内容" show-overflow-tooltip />
            <el-table-column prop="createTime" label="时间" width="180" />
          </el-table>
        </el-tab-pane>

        <el-tab-pane label="摘要记忆（summary）" name="summary">
          <div v-if="summaryGraph" class="summary-block">
            <div class="summary-header">
              <el-icon><Reading /></el-icon>
              <span class="title">记忆图谱摘要</span>
              <el-tag size="small">{{ summaryGraph.nodes?.length || 0 }} 节点</el-tag>
            </div>
            <pre class="json">{{ JSON.stringify(summaryGraph, null, 2) }}</pre>
          </div>
          <el-empty v-else description="暂无摘要数据。点击「摘要记忆」生成。" />
        </el-tab-pane>
      </el-tabs>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { MagicStick, Document, Reading } from '@element-plus/icons-vue'
import api from '../api'

const deviceId = ref('ESP32-001')
const kw = ref('')
const activeTab = ref('chat')

const chatRows = ref([])
const summaryGraph = ref(null)
const loading = ref(false)
const extracting = ref(false)
const summarizing = ref(false)

async function load() {
  if (!deviceId.value) { ElMessage.warning('请输入 deviceId'); return }
  loading.value = true
  try {
    const r = await api.memory(deviceId.value)
    let list = r?.data?.items || r?.data?.data?.items || r?.data || []
    chatRows.value = kw.value ? list.filter(x => JSON.stringify(x).includes(kw.value)) : list
  } catch (e) { ElMessage.error(e.message) }
  finally { loading.value = false }

  // 同时拉摘要图谱
  try {
    const g = await api.memoryGraph(deviceId.value)
    summaryGraph.value = g?.data?.data || g?.data || null
  } catch (_) { /* 摘要图谱可能还没建 */ }
}

async function doExtract() {
  extracting.value = true
  try {
    await api.extractMemory(deviceId.value)
    ElMessage.success('已提交提取任务，后台异步处理')
    setTimeout(load, 2000)
  } catch (e) { ElMessage.error(e.message) }
  finally { extracting.value = false }
}

async function doSummarize() {
  summarizing.value = true
  try {
    await api.summarizeMemory(deviceId.value)
    ElMessage.success('已提交摘要任务，后台异步处理')
    setTimeout(load, 2000)
  } catch (e) { ElMessage.error(e.message) }
  finally { summarizing.value = false }
}

onMounted(load)
</script>

<style scoped>
.header-row { display:flex; justify-content:space-between; align-items:center; gap:12px; }
.toolbar, .actions { display:flex; gap:8px; align-items:center; }
.memory-tabs { margin-top: -8px; }
.summary-block { padding: 12px; }
.summary-header { display:flex; gap:8px; align-items:center; margin-bottom: 12px; }
.summary-header .title { font-weight: 600; font-size: 15px; }
.json {
  background: #0f172a; color: #cbd5e1;
  padding: 12px; border-radius: 6px;
  font-size: 12px; line-height: 1.5;
  max-height: 600px; overflow: auto;
}
</style>
