<script setup lang="ts">
import type { DataTableColumns } from 'naive-ui';

import type { SystemLogApi } from '#/api/system/log';

import { computed, h, onMounted, reactive, ref } from 'vue';

import { Page } from '@vben/common-ui';

import {
  NButton,
  NCard,
  NDataTable,
  NDatePicker,
  NInput,
  NSelect,
  NSpace,
  NTag,
} from 'naive-ui';

import { getLoginLogsApi } from '#/api/system/log';

const loading = ref(false);
const rows = ref<SystemLogApi.LoginLog[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);

const successOptions = [
  { label: '成功', value: 1 },
  { label: '失败', value: 0 },
];

const query = reactive<{
  keyword: string;
  range: [number, number] | null;
  success: null | number;
}>({
  keyword: '',
  range: null,
  success: null,
});

function toISOString(value: null | number | undefined) {
  return value ? new Date(value).toISOString() : undefined;
}

async function loadData() {
  loading.value = true;
  try {
    const result = await getLoginLogsApi({
      // 结束时间取当天 23:59:59，避免只查到 00:00:00
      from: toISOString(query.range?.[0]),
      keyword: query.keyword || undefined,
      page: page.value,
      pageSize: pageSize.value,
      success: query.success ?? undefined,
      to: toISOString(query.range?.[1] ? query.range[1] + 86_399_999 : undefined),
    });
    rows.value = result.list;
    total.value = result.total;
  } finally {
    loading.value = false;
  }
}

function handleSearch() {
  page.value = 1;
  loadData();
}

function handleReset() {
  query.keyword = '';
  query.range = null;
  query.success = null;
  handleSearch();
}

function formatTime(value: string) {
  return value ? value.replace('T', ' ').slice(0, 19) : '-';
}

const columns: DataTableColumns<SystemLogApi.LoginLog> = [
  { key: 'id', title: 'ID', width: 80 },
  { key: 'username', title: '用户名', minWidth: 120 },
  {
    key: 'success',
    title: '结果',
    width: 90,
    render: (row) =>
      h(
        NTag,
        { size: 'small', type: row.success ? 'success' : 'error' },
        { default: () => (row.success ? '成功' : '失败') },
      ),
  },
  { key: 'message', title: '描述', minWidth: 180 },
  { key: 'ip', title: 'IP', minWidth: 130 },
  {
    key: 'userAgent',
    title: 'User-Agent',
    minWidth: 240,
    ellipsis: { tooltip: true },
  },
  {
    key: 'createdAt',
    title: '登录时间',
    width: 180,
    render: (row) => formatTime(row.createdAt),
  },
];

const pagination = computed(() => ({
  itemCount: total.value,
  page: page.value,
  pageSize: pageSize.value,
  pageSizes: [20, 50, 100],
  showSizePicker: true,
  onChange: (value: number) => {
    page.value = value;
    loadData();
  },
  onUpdatePageSize: (value: number) => {
    pageSize.value = value;
    page.value = 1;
    loadData();
  },
}));

onMounted(loadData);
</script>

<template>
  <Page description="记录每一次登录尝试，包含失败原因与来源 IP" title="登录日志">
    <NCard size="small">
      <template #header>
        <NSpace>
          <NInput
            v-model:value="query.keyword"
            clearable
            placeholder="用户名 / IP"
            style="width: 180px"
            @keyup.enter="handleSearch"
          />
          <NSelect
            v-model:value="query.success"
            clearable
            :options="successOptions"
            placeholder="结果"
            style="width: 110px"
          />
          <NDatePicker
            v-model:value="query.range"
            clearable
            type="daterange"
            style="width: 260px"
          />
          <NButton type="primary" @click="handleSearch">查询</NButton>
          <NButton @click="handleReset">重置</NButton>
        </NSpace>
      </template>

      <NDataTable
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="pagination"
        :remote="true"
        :row-key="(row: SystemLogApi.LoginLog) => row.id"
      />
    </NCard>
  </Page>
</template>
