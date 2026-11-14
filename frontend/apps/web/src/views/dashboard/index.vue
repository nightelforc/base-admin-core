<script setup lang="ts">
import type { DashboardApi } from '#/api/system/dashboard';

import { computed, onMounted, ref } from 'vue';

import { Page } from '@vben/common-ui';

import {
  NCard,
  NGi,
  NGrid,
  NProgress,
  NSpin,
  NStatistic,
  NTag,
} from 'naive-ui';

import { getDashboardOverviewApi } from '#/api/system/dashboard';

const loading = ref(false);
const overview = ref<DashboardApi.Overview | null>(null);

const trend = computed(() => overview.value?.trend ?? []);
const maxTrend = computed(() =>
  Math.max(1, ...trend.value.map((item) => item.total)),
);
const loginSuccessRate = computed(() => {
  const item = overview.value;
  if (!item || item.loginToday === 0) {
    return 0;
  }
  return Math.round(
    ((item.loginToday - item.loginFailedToday) / item.loginToday) * 100,
  );
});

async function loadData() {
  loading.value = true;
  try {
    overview.value = await getDashboardOverviewApi();
  } finally {
    loading.value = false;
  }
}

onMounted(loadData);
</script>

<template>
  <Page description="系统关键指标与近 7 天登录趋势" title="工作台">
    <NSpin :show="loading">
      <div class="flex flex-col gap-4">
        <NGrid cols="1 s:2 l:4" :x-gap="16" :y-gap="16" responsive="screen">
          <NGi>
            <NCard size="small" title="用户总数">
              <NStatistic :value="overview?.userTotal ?? 0">
                <template #suffix>
                  <span class="text-sm text-gray-400">
                    / 启用 {{ overview?.userEnabled ?? 0 }}
                  </span>
                </template>
              </NStatistic>
            </NCard>
          </NGi>
          <NGi>
            <NCard size="small" title="角色数量">
              <NStatistic :value="overview?.roleTotal ?? 0" />
            </NCard>
          </NGi>
          <NGi>
            <NCard size="small" title="菜单数量">
              <NStatistic :value="overview?.menuTotal ?? 0" />
            </NCard>
          </NGi>
          <NGi>
            <NCard size="small" title="今日操作数">
              <NStatistic :value="overview?.operationToday ?? 0" />
            </NCard>
          </NGi>
        </NGrid>

        <NGrid cols="1 l:2" :x-gap="16" :y-gap="16" responsive="screen">
          <NGi>
            <NCard size="small" title="今日登录">
              <div class="flex flex-col gap-3">
                <div class="flex items-center justify-between">
                  <NStatistic :value="overview?.loginToday ?? 0" />
                  <NTag :type="loginSuccessRate >= 90 ? 'success' : 'warning'">
                    成功率 {{ loginSuccessRate }}%
                  </NTag>
                </div>
                <NProgress
                  :percentage="loginSuccessRate"
                  :show-indicator="false"
                  :status="loginSuccessRate >= 90 ? 'success' : 'warning'"
                />
                <div class="text-xs text-gray-400">
                  失败次数：{{ overview?.loginFailedToday ?? 0 }}
                </div>
              </div>
            </NCard>
          </NGi>
          <NGi>
            <NCard size="small" title="近 7 天登录趋势">
              <div class="flex h-32 items-end gap-3">
                <div
                  v-for="item in trend"
                  :key="item.date"
                  class="flex flex-1 flex-col items-center gap-1"
                >
                  <div class="text-xs text-gray-400">{{ item.total }}</div>
                  <div
                    class="w-6 rounded-t bg-primary/70"
                    :style="{
                      height: `${Math.max(4, (item.total / maxTrend) * 80)}px`,
                    }"
                  ></div>
                  <div class="text-xs text-gray-400">
                    {{ item.date.slice(5) }}
                  </div>
                </div>
                <div v-if="trend.length === 0" class="text-sm text-gray-400">
                  暂无数据
                </div>
              </div>
            </NCard>
          </NGi>
        </NGrid>
      </div>
    </NSpin>
  </Page>
</template>
