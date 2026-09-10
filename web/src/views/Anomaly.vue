<template><div class="page">
  <el-card><template #header><span>异常检测规则</span></template>
    <el-table :data="rules" border stripe size="small">
      <el-table-column prop="id" label="ID" width="80" />
      <el-table-column prop="name" label="规则" />
      <el-table-column prop="metric" label="指标" />
      <el-table-column prop="level" label="等级" width="80" />
      <el-table-column label="启用" width="100">
        <template #default="{ row }"><el-tag :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? '启用' : '停用' }}</el-tag></template>
      </el-table-column>
    </el-table>
  </el-card>
  <el-card style="margin-top:16px"><template #header><span>近期异常事件</span></template>
    <el-table :data="events" border stripe size="small">
      <el-table-column prop="id" label="ID" width="100" />
      <el-table-column prop="deviceId" label="设备" />
      <el-table-column prop="ruleName" label="规则" />
      <el-table-column prop="severity" label="等级" width="80" />
      <el-table-column prop="message" label="消息" show-overflow-tooltip />
      <el-table-column prop="firedAt" label="时间" width="180" />
    </el-table>
  </el-card>
</div></template>
<script setup>
import { ref, onMounted } from 'vue'
const rules = ref([
  { id: 1, name: '设备离线超时', metric: 'device_offline_minutes', level: 'warning', enabled: true },
  { id: 2, name: 'LLM 调用失败率', metric: 'llm_failure_rate', level: 'error', enabled: true },
  { id: 3, name: 'ASR 识别延迟', metric: 'asr_latency_p99', level: 'warning', enabled: false },
])
const events = ref([])
onMounted(() => { /* TODO: 调 /api/v1/anomaly/events */ })
</script>