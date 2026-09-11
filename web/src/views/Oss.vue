<template>
  <div class="oss-page">
    <el-card>
      <template #header>
        <div class="header-row">
          <span>OSS 对象存储配置</span>
          <el-button type="primary" @click="openCreate">+ 新增配置</el-button>
        </div>
      </template>
      <el-table :data="rows" border stripe v-loading="loading">
        <el-table-column prop="id" label="ID" width="80" />
        <el-table-column prop="provider" label="provider" width="120">
          <template #default="{ row }">
            <el-tag size="small">{{ row.provider }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="configName" label="配置名" min-width="160" />
        <el-table-column prop="endpoint" label="endpoint" min-width="220" show-overflow-tooltip>
          <template #default="{ row }"><code class="code">{{ row.endpoint }}</code></template>
        </el-table-column>
        <el-table-column prop="bucket" label="bucket" width="160" />
        <el-table-column prop="region" label="region" width="100" />
        <el-table-column prop="pathPrefix" label="路径前缀" width="140" />
        <el-table-column label="默认" width="80">
          <template #default="{ row }">
            <el-tag v-if="row.isDefault" type="success" size="small">默认</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="100">
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

    <el-dialog v-model="dialogVisible" :title="form.id ? '编辑 OSS 配置' : '新增 OSS 配置'" width="600px">
      <el-form :model="form" label-width="110px">
        <el-form-item label="provider" required>
          <el-select v-model="form.provider" style="width:100%">
            <el-option label="阿里云 OSS" value="aliyun" />
            <el-option label="AWS S3" value="aws" />
            <el-option label="腾讯云 COS" value="tencent" />
            <el-option label="MinIO" value="minio" />
          </el-select>
        </el-form-item>
        <el-form-item label="配置名" required>
          <el-input v-model="form.configName" placeholder="如 prod-aliyun / dev-minio" :disabled="!!form.id" />
        </el-form-item>
        <el-form-item label="endpoint" required>
          <el-input v-model="form.endpoint" placeholder="oss-cn-beijing.aliyuncs.com" />
        </el-form-item>
        <el-form-item label="bucket" required>
          <el-input v-model="form.bucket" />
        </el-form-item>
        <el-form-item label="region">
          <el-input v-model="form.region" placeholder="cn-beijing" />
        </el-form-item>
        <el-form-item label="路径前缀">
          <el-input v-model="form.pathPrefix" placeholder="aisaas/" />
        </el-form-item>
        <el-form-item label="AccessKey" required>
          <el-input v-model="form.accessKey" placeholder="AK ID（明文）" />
        </el-form-item>
        <el-form-item label="Secret" :required="!form.id">
          <el-input v-model="form.secret" type="password" show-password placeholder="AK Secret（AES 加密）" />
        </el-form-item>
        <el-form-item label="说明">
          <el-input v-model="form.configDesc" type="textarea" :rows="2" />
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
const dialogVisible = ref(false)
const form = reactive({
  id: 0, provider: 'aliyun', configName: '', configDesc: '',
  endpoint: '', bucket: '', accessKey: '', secret: '',
  region: '', pathPrefix: '', isDefault: false, status: 1
})

async function load() {
  loading.value = true
  try { const r = await api.ossConfigs(); rows.value = r.data || [] }
  catch (e) { ElMessage.error(e.message || '加载失败') }
  finally { loading.value = false }
}
function openCreate() {
  Object.assign(form, {
    id: 0, provider: 'aliyun', configName: '', configDesc: '',
    endpoint: '', bucket: '', accessKey: '', secret: '',
    region: '', pathPrefix: '', isDefault: false, status: 1
  })
  dialogVisible.value = true
}
function openEdit(row) {
  Object.assign(form, { ...row, secret: '' })
  dialogVisible.value = true
}
async function save() {
  try {
    if (form.id) {
      const body = { ...form }; delete body.id
      if (!body.secret) delete body.secret
      await api.updateOss(form.id, body)
    } else {
      await api.createOss(form)
    }
    ElMessage.success('已保存'); dialogVisible.value = false; load()
  } catch (e) { ElMessage.error(e.message) }
}
async function toggleStatus(row) {
  try { await api.updateOss(row.id, { status: row.status }) }
  catch (e) { ElMessage.error(e.message); row.status = row.status === 1 ? 0 : 1 }
}
async function remove(row) {
  try { await ElMessageBox.confirm(`确认删除 OSS 配置「${row.configName}」？`) } catch { return }
  try { await api.deleteOss(row.id); ElMessage.success('已删除'); load() }
  catch (e) { ElMessage.error(e.message) }
}
onMounted(load)
</script>

<style scoped>
.header-row { display:flex; justify-content:space-between; align-items:center; }
.code { font-family:monospace; font-size:12px; color:#475569; }
</style>
