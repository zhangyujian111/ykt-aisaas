<template>
  <div>
    <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 16px">
      <h2 style="margin: 0">我的设备</h2>
      <el-button type="primary" @click="bindVisible = true">＋ 绑定新设备</el-button>
    </div>

    <el-empty v-if="!devices.length" description="还没有绑定设备。设备首次联网后，用设备页面的 6 位绑定码添加。" />

    <el-row :gutter="16">
      <el-col :span="12" v-for="d in devices" :key="d.tenantId" style="margin-bottom: 16px">
        <el-card>
          <template #header>
            <b>{{ d.bindName || d.deviceId }}</b>
            <el-tag size="small" style="margin-left: 8px">{{ planName(d.planId) }}</el-tag>
            <span v-if="d.planExpire" style="float: right; font-size: 12px; color: #999">
              {{ d.planExpire.slice(0, 10) }} 到期
            </span>
          </template>

          <el-descriptions :column="2" size="small">
            <el-descriptions-item label="设备 ID">{{ d.deviceId }}</el-descriptions-item>
            <el-descriptions-item label="余额">
              <b style="color: #e6a23c">¥{{ (d.balance.balanceCents / 100).toFixed(2) }}</b>
            </el-descriptions-item>
          </el-descriptions>

          <div style="margin: 12px 0" v-for="dim in dims(d)" :key="dim.k">
            <div style="font-size: 12px; color: #666; margin-bottom: 4px">{{ dim.label }}：{{ dim.used }}</div>
            <el-progress :percentage="dim.pct" :stroke-width="8" :show-text="false" />
          </div>

          <div style="margin-top: 16px">
            <el-button size="small" @click="openSubscribe(d)">订阅套餐</el-button>
            <el-button size="small" type="warning" @click="openRecharge(d)">充值</el-button>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-dialog v-model="bindVisible" title="绑定新设备" width="360px">
      <el-input v-model="bindCode" placeholder="输入设备 6 位绑定码" maxlength="6" style="margin-bottom: 12px" />
      <el-input v-model="bindName" placeholder="设备别名（如：客厅小智）" style="margin-bottom: 12px" />
      <el-button type="primary" style="width: 100%" @click="doBind">绑定</el-button>
    </el-dialog>

    <el-dialog v-model="subVisible" title="选择套餐" width="420px">
      <el-radio-group v-model="pickedPlan" style="display: flex; flex-direction: column; gap: 12px">
        <el-radio v-for="p in plans" :key="p.id" :value="p.id" border style="margin: 0; width: 100%">
          {{ p.name }} — ¥{{ Number(p.priceMonthly).toFixed(0) }}/月
          <div style="font-size: 12px; color: #999; margin-left: 24px">{{ quotaText(p) }}</div>
        </el-radio>
      </el-radio-group>
      <template #footer>
        <el-button @click="subVisible = false">取消</el-button>
        <el-button type="primary" :loading="acting" @click="doSubscribe">确认订阅</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="rcVisible" title="账户充值" width="420px">
      <div style="text-align: center">
        <div style="margin-bottom: 12px; color: #666">当前余额 ¥{{ (rcTarget?.balance.balanceCents / 100).toFixed(2) }}</div>
        <el-radio-group v-model="rcAmount">
          <el-radio-button :value="1000">¥10</el-radio-button>
          <el-radio-button :value="5000">¥50</el-radio-button>
          <el-radio-button :value="10000">¥100</el-radio-button>
          <el-radio-button :value="50000">¥500</el-radio-button>
        </el-radio-group>
      </div>
      <template #footer>
        <el-button @click="rcVisible = false">取消</el-button>
        <el-button type="warning" :loading="acting" @click="doRecharge">去支付</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="payVisible" title="订单支付" width="380px">
      <div style="text-align: center; padding: 8px 0">
        <div style="font-size: 28px; color: #e6a23c; margin-bottom: 8px">¥{{ (pendingOrder?.amountCents / 100).toFixed(2) }}</div>
        <div style="color: #999; font-size: 13px">订单号 {{ pendingOrder?.orderNo }}</div>
        <div style="color: #999; font-size: 13px; margin: 4px 0 12px">（演示环境：点击下方按钮模拟支付成功）</div>
        <el-button type="success" :loading="acting" @click="doPay">我已支付 ¥{{ (pendingOrder?.amountCents / 100).toFixed(2) }}</el-button>
      </div>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import api from '../api'

const devices = ref([])
const plans = ref([])
const bindVisible = ref(false)
const bindCode = ref('')
const bindName = ref('')
const subVisible = ref(false)
const rcVisible = ref(false)
const payVisible = ref(false)
const pickedPlan = ref(null)
const rcAmount = ref(5000)
const rcTarget = ref(null)
const subTarget = ref(null)
const pendingOrder = ref(null)
const acting = ref(false)

const DIM_LABELS = {
  llm_tokens_in: 'AI 对话（输入 token）',
  llm_tokens_out: 'AI 对话（输出 token）',
  tts_chars: '语音合成（字符）',
  asr_seconds: '语音识别（秒）',
}

function dims(d) {
  const u = d.usage?.dimensions || {}
  return Object.entries(DIM_LABELS)
    .filter(([k]) => u[k]?.used > 0)
    .map(([k, label]) => ({
      k, label, used: u[k].used,
      pct: Math.min(100, Math.round((u[k].used / (u[k].used + (d.usage?.quotaRemaining?.[k] ?? 0) || 1)) * 100)),
    }))
}

function planName(id) {
  const p = plans.value.find((x) => Number(x.id) === Number(id))
  return p ? p.name : id === 1 ? '免费版' : '未订阅'
}

function quotaText(p) {
  try {
    const q = JSON.parse(p.quotas)
    const m = q.llm_tokens_in ? `对话 ${fmtNum(q.llm_tokens_in)} token` : ''
    const t = q.tts_chars ? ` · 语音 ${fmtNum(q.tts_chars)} 字` : ''
    return m + t
  } catch { return '' }
}
function fmtNum(n) { return n >= 10000 ? (n / 10000) + '万' : n }

async function load() {
  devices.value = await api.devices()
  plans.value = await api.plans()
}

async function doBind() {
  try {
    await api.bind({ bindCode: bindCode.value, bindName: bindName.value })
    ElMessage.success('绑定成功')
    bindVisible.value = false
    bindCode.value = bindName.value = ''
    load()
  } catch (e) { ElMessage.error(e.message || '绑定失败') }
}

function openSubscribe(d) { subTarget.value = d; pickedPlan.value = null; subVisible.value = true }
async function doSubscribe() {
  if (!pickedPlan.value) return ElMessage.warning('请选择套餐')
  acting.value = true
  try {
    await api.subscribe(subTarget.value.tenantId, pickedPlan.value)
    ElMessage.success('订阅成功')
    subVisible.value = false
    load()
  } catch (e) { ElMessage.error(e.message || '订阅失败') } finally { acting.value = false }
}

function openRecharge(d) { rcTarget.value = d; rcVisible.value = true }
async function doRecharge() {
  acting.value = true
  try {
    pendingOrder.value = await api.recharge(rcTarget.value.tenantId, rcAmount.value)
    rcVisible.value = false
    payVisible.value = true
  } catch (e) { ElMessage.error(e.message || '下单失败') } finally { acting.value = false }
}

async function doPay() {
  acting.value = true
  try {
    await api.payMock(pendingOrder.value.id)
    ElMessage.success('支付成功，余额已到账')
    payVisible.value = false
    load()
  } catch (e) { ElMessage.error(e.message || '支付失败') } finally { acting.value = false }
}

onMounted(load)
</script>
