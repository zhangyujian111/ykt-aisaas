<template>
  <div class="chat-page">
    <el-card>
      <template #header>
        <div class="header-row">
          <span>Web 调试对话</span>
          <div class="toolbar">
            <el-select v-model="form.model" style="width:200px" placeholder="选择模型">
              <el-option v-for="m in modelList" :key="m.id" :label="m.id" :value="m.id" />
            </el-select>
            <el-button @click="loadModels" :loading="loadingModels">
              <el-icon><Refresh /></el-icon> 刷新
            </el-button>
            <el-switch v-model="stream" active-text="流式" inactive-text="非流式" />
            <el-switch v-model="useRag" active-text="RAG" inactive-text="直答" />
          </div>
        </div>
      </template>

      <div class="chat-area">
        <div class="messages" ref="msgBoxRef">
          <div v-for="(m, i) in messages" :key="i" :class="['msg', m.role]">
            <div class="role">{{ m.role === 'user' ? '我' : 'AI' }}</div>
            <div class="bubble">
              <div class="content" v-html="formatContent(m.content)"></div>
            </div>
          </div>
          <div v-if="loading" class="msg assistant">
            <div class="role">AI</div>
            <div class="bubble">
              <div class="content"><span class="dot-flashing"></span></div>
            </div>
          </div>
        </div>

        <div class="input-row">
          <el-input v-model="input" type="textarea" :rows="3"
            placeholder="输入消息，Enter 发送，Shift+Enter 换行"
            @keydown.enter.exact.prevent="send" resize="none" />
          <el-button type="primary" :loading="loading" @click="send" style="height:auto">
            <el-icon><Promotion /></el-icon> 发送
          </el-button>
          <el-button @click="clearChat" style="height:auto">清空</el-button>
        </div>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, nextTick } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, Promotion } from '@element-plus/icons-vue'
import api from '../api'

const modelList = ref([])
const loadingModels = ref(false)
const form = reactive({ model: 'qwen3.6-flash' })
const stream = ref(false)
const useRag = ref(false)
const messages = ref([])
const input = ref('')
const loading = ref(false)
const msgBoxRef = ref(null)

async function loadModels() {
  loadingModels.value = true
  try {
    const r = await api.modelList()
    const arr = r?.data || r || []
    modelList.value = Array.isArray(arr) ? arr : []
    if (modelList.value.length && !modelList.value.find(m => m.id === form.model)) {
      form.model = modelList.value[0].id
    }
  } catch (e) { ElMessage.error('加载模型失败：' + (e.message || '')) }
  finally { loadingModels.value = false }
}

async function send() {
  const text = input.value.trim()
  if (!text || loading.value) return
  messages.value.push({ role: 'user', content: text })
  input.value = ''
  await scrollBottom()
  loading.value = true
  const userMsg = text
  try {
    if (useRag.value) {
      // 走 RAG 检索增强：先把消息作为 KB query，再调 chat
      // MVP: 直接发消息，提示用户去知识库页面测试检索
    }
    if (stream.value) {
      await sendStream(userMsg)
    } else {
      const r = await api.chat({
        model: form.model,
        messages: messages.value.map(m => ({ role: m.role, content: m.content })),
        stream: false, max_tokens: 1024
      })
      const reply = r?.choices?.[0]?.message?.content || r?.data?.choices?.[0]?.message?.content || '(空)'
      messages.value.push({ role: 'assistant', content: reply })
      await scrollBottom()
    }
  } catch (e) {
    messages.value.push({ role: 'assistant', content: '**调用失败**：' + (e.message || JSON.stringify(e)) })
  } finally {
    loading.value = false
    await scrollBottom()
  }
}

async function sendStream(userMsg) {
  const idx = messages.value.length
  messages.value.push({ role: 'assistant', content: '' })
  return new Promise((resolve) => {
    api.chatStream({
      model: form.model,
      messages: [{ role: 'user', content: userMsg }],
      stream: true, max_tokens: 1024
    }).then(res => {
      const reader = res.data.getReader?.()
      const decoder = new TextDecoder('utf-8')
      const pump = ({ done, value }) => {
        if (done) return resolve()
        const chunk = decoder.decode(value)
        const lines = chunk.split('\n').filter(l => l.startsWith('data:'))
        for (const line of lines) {
          const payload = line.replace(/^data:\s*/, '').trim()
          if (payload === '[DONE]') continue
          try {
            const json = JSON.parse(payload)
            const delta = json.choices?.[0]?.delta?.content || ''
            if (delta) {
              messages.value[idx].content += delta
              scrollBottom()
            }
          } catch (_) {}
        }
        reader.read().then(pump)
      }
      if (reader) reader.read().then(pump)
      else resolve()
    }).catch(err => {
      messages.value[idx].content = '**流式调用失败**：' + (err.message || '')
      resolve()
    })
  })
}

function clearChat() { messages.value = [] }

async function scrollBottom() {
  await nextTick()
  const el = msgBoxRef.value
  if (el) el.scrollTop = el.scrollHeight
}

function formatContent(s) {
  if (!s) return ''
  // 极简 markdown：代码块 + 换行 + 加粗
  return s
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
    .replace(/`([^`]+)`/g, '<code>$1</code>')
    .replace(/\n/g, '<br>')
}

onMounted(loadModels)
</script>

<style scoped>
.header-row { display:flex; justify-content:space-between; align-items:center; gap:12px; }
.toolbar { display:flex; gap:12px; align-items:center; }
.chat-area { display:flex; flex-direction:column; height: 65vh; }
.messages { flex:1; overflow-y:auto; padding: 16px; background: #f8fafc; border-radius: 6px; }
.msg { display:flex; gap:12px; margin-bottom: 16px; align-items:flex-start; }
.msg.user { flex-direction: row-reverse; }
.msg .role { font-weight: 600; font-size: 13px; color: #475569; min-width: 24px; }
.bubble { max-width: 75%; padding: 10px 14px; border-radius: 8px; font-size: 14px; line-height: 1.6; word-break: break-word; }
.msg.user .bubble { background: #3b82f6; color: #fff; }
.msg.assistant .bubble { background: #fff; border: 1px solid #e2e8f0; }
.input-row { display:flex; gap:8px; align-items:flex-end; padding-top: 12px; }
.input-row :deep(.el-textarea__inner) { font-family: inherit; }

.dot-flashing { display:inline-block; width:8px; height:8px; border-radius:50%; background:#94a3b8; margin-right:6px; animation: dot-flashing 1s infinite linear alternate; }
@keyframes dot-flashing { 0% { opacity: 0.2; } 50% { opacity: 1; } 100% { opacity: 0.2; } }
</style>
