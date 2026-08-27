<template>
  <div>
    <h2>充值记录</h2>
    <el-table :data="orders" stripe>
      <el-table-column prop="orderNo" label="订单号" width="240" />
      <el-table-column label="金额">
        <template #default="{ row }">¥{{ (row.amountCents / 100).toFixed(2) }}</template>
      </el-table-column>
      <el-table-column label="状态">
        <template #default="{ row }">
          <el-tag :type="row.status === 1 ? 'success' : row.status === 2 ? 'info' : 'warning'" size="small">
            {{ ['待支付', '已支付', '已取消'][row.status] }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="createTime" label="时间" width="200">
        <template #default="{ row }">{{ (row.createTime || '').replace('T', ' ').slice(0, 19) }}</template>
      </el-table-column>
    </el-table>
  </div>
</template>
<script setup>
import { ref, onMounted } from 'vue'
import api from '../api'
const orders = ref([])
onMounted(async () => { orders.value = await api.orders() })
</script>
