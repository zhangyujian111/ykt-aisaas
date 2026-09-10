<template>
  <div class="page">
    <el-row :gutter="16">
      <el-col :span="6"><el-card><div class="metric-label">账户余额</div><div class="metric-value">¥{{ balanceYuan }}</div></el-card></el-col>
      <el-col :span="6"><el-card><div class="metric-label">本月 LLM 令牌入</div><div class="metric-value">{{ llmInUsed }}</div></el-card></el-col>
      <el-col :span="6"><el-card><div class="metric-label">本月 LLM 令牌出</div><div class="metric-value">{{ llmOutUsed }}</div></el-card></el-col>
      <el-col :span="6"><el-card><div class="metric-label">租户 ID</div><div class="metric-value">{{ usage.balance?.tenantId ?? '—' }}</div></el-card></el-col>
    </el-row>
    <el-card style="margin-top:16px"><template #header><span>用量维度（本月）</span></template>
      <el-table :data="dimRows" border size="small">
        <el-table-column prop="key" label="指标" width="200" />
        <el-table-column prop="used" label="用量" width="200" />
        <el-table-column prop="cost" label="消耗（元）" width="160" />
      </el-table>
    </el-card>
    <el-card style="margin-top:16px"><template #header><span>API Key 列表</span></template>
      <el-table :data="keys" border size="small">
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="name" label="名称" width="160" />
        <el-table-column prop="keyPrefix" label="前缀" width="120" />
        <el-table-column prop="scope" label="Scope" />
        <el-table-column prop="status" label="状态" width="80">
          <template #default="{ row }">
            <el-tag :type="row.status===1?'success':'danger'" size="small">{{ row.status===1?'启用':'禁用' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="createTime" label="创建时间" width="180" />
      </el-table>
    </el-card>
  </div>
</template>
<script setup>
import { ref, computed, onMounted } from 'vue'
import * as echarts from 'echarts'
import { ElMessage } from 'element-plus'
import api from '../api'
const balance = ref(null); const usage = ref({}); const keys = ref([])
const balanceYuan = computed(() => balance.value ? (balance.value.balanceCents / 100).toFixed(2) : '—')
const llmInUsed = computed(() => usage.value?.dimensions?.llm_tokens_in?.used ?? '—')
const llmOutUsed = computed(() => usage.value?.dimensions?.llm_tokens_out?.used ?? '—')
const dimRows = computed(() => {
  const dims = usage.value?.dimensions || {}
  return Object.entries(dims).map(([k, v]) => ({
    key: k,
    used: v?.used ?? 0,
    cost: v?.costCents != null ? (v.costCents / 100).toFixed(2) : '0.00'
  }))
})
onMounted(async () => {
  try { balance.value = await api.balance() } catch (e) { console.error('balance', e) }
  try { usage.value = await api.usageOverview() || {} } catch (e) { console.error('usage', e); ElMessage.error('用量数据加载失败：' + (e.message || '')) }
  try { keys.value = await api.apikeys() || [] } catch (e) { console.error('apikeys', e) }
})
</script>
<style scoped>.metric-label{font-size:13px;color:#94a3b8;margin-bottom:8px}.metric-value{font-size:24px;font-weight:600;color:#0f172a}</style>