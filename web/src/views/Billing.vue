<template><div class="page">
  <el-card><template #header><span>账单总览</span></template>
    <el-row :gutter="16">
      <el-col :span="8"><el-card><div class="metric-label">余额</div><div class="metric-value">¥{{ balance?.balance ?? '—' }}</div></el-card></el-col>
      <el-col :span="8"><el-card><div class="metric-label">已消耗</div><div class="metric-value">¥{{ balance?.consumed ?? '—' }}</div></el-card></el-col>
      <el-col :span="8"><el-card><div class="metric-label">充值总额</div><div class="metric-value">¥{{ balance?.totalRecharged ?? '—' }}</div></el-card></el-col>
    </el-row>
  </el-card>
</div></template>
<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../api'
const balance = ref(null)
onMounted(async () => { try { balance.value = await api.balance() } catch (e) { ElMessage.error(e.message) } })
</script>
<style scoped>.metric-label{font-size:13px;color:#94a3b8;margin-bottom:8px}.metric-value{font-size:24px;font-weight:600;color:#0f172a}</style>