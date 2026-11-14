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
  NModal,
  NSpace,
  NTag,
} from 'naive-ui';

import { getOperationLogsApi } from '#/api/system/log';

const loading = ref(false);
const rows = ref<SystemLogApi.OperationLog[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);

const query = reactive<{ keyword: string; range: [number, number] | null }>({
  keyword: '',
  range: null,
});

const detailVisible = ref(false);
const detail = ref<null | SystemLogApi.OperationLog>(null);

function toISOString(value: null | number | undefined) {
  return value ? new Date(value).toISOString() : undefined;
}

async function loadData() {
  loading.value = true;
  try {
    const result = await getOperationLogsApi({
      from: toISOString(query.range?.[0]),
      keyword: query.keyword || undefined,
      page: page.value,
      pageSize: pageSize.value,
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
  handleSearch();
}

function openDetail(row: SystemLogApi.OperationLog) {
  detail.value = row;
  detailVisible.value = true;
}

function formatTime(value: string) {
  return value ? value.replace('T', ' ').slice(0, 19) : '-';
}

const columns: DataTableColumns<SystemLogApi.OperationLog> = [
  { key: 'id', title: 'ID', width: 80 },
  { key: 'username', title: '操作人', minWidth: 110 },
  {
    key: 'method',
    title: '方法',
    width: 90,
    render: (row) =>
      h(
        NTag,
        {
          size: 'small',
          type: row.method === 'DELETE' ? 'error' : row.method === 'POST' ? 'success' : 'info',
        },
        { default: () => row.method },
      ),
  },
  { key: 'path', title: '接口', minWidth: 240, ellipsis: { tooltip: true } },
  {
    key: 'statusCode',
    title: '状态码',
    width: 100,
    render: (row) =>
      h(
        NTag,
        { size: 'small', type: row.statusCode < 400 ? 'success' : 'error' },
        { default: () => String(row.statusCode) },
      ),
  },
  { key: 'latencyMs', title: '耗时(ms)', width: 110 },
  { key: 'ip', title: 'IP', minWidth: 130 },
  {
    key: 'createdAt',
    title: '操作时间',
    width: 180,
    render: (row) => formatTime(row.createdAt),
  },
  {
    key: 'actions',
    title: '操作',
    width: 90,
    render: (row) =>
      h(
        NButton,
        { size: 'small', text: true, type: 'primary', onClick: () => openDetail(row) },
        { default: () => '详情' },
      ),
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
  <Page description="记录后台写操作，可用于审计与问题排查" title="操作日志">
    <NCard size="small">
      <template #header>
        <NSpace>
          <NInput
            v-model:value="query.keyword"
            clearable
            placeholder="操作人 / 接口路径"
            style="width: 220px"
            @keyup.enter="handleSearch"
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
        :row-key="(row: SystemLogApi.OperationLog) => row.id"
        :scroll-x="1300"
      />
    </NCard>

    <NModal
      v-model:show="detailVisible"
      preset="card"
      style="width: 680px"
      title="操作详情"
    >
      <div class="flex flex-col gap-3 text-sm">
        <div><span class="text-gray-400">操作人：</span>{{ detail?.username }}</div>
        <div><span class="text-gray-400">接口：</span>{{ detail?.method }} {{ detail?.path }}</div>
        <div><span class="text-gray-400">状态码：</span>{{ detail?.statusCode }}</div>
        <div><span class="text-gray-400">耗时：</span>{{ detail?.latencyMs }} ms</div>
        <div><span class="text-gray-400">IP：</span>{{ detail?.ip }}</div>
        <div><span class="text-gray-400">User-Agent：</span>{{ detail?.userAgent }}</div>
        <div>
          <div class="mb-1 text-gray-400">请求体：</div>
          <pre
            class="max-h-72 overflow-auto rounded-md border border-border bg-muted p-3 text-xs leading-relaxed break-all whitespace-pre-wrap"
            >{{ detail?.requestBody || '-' }}</pre
          >
        </div>
      </div>
    </NModal>
  </Page>
</template>
