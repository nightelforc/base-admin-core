<script setup lang="ts">
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';
import type { TreeOption } from 'naive-ui';

import type { SystemMenuApi } from '#/api/system/menu';
import type { SystemRoleApi } from '#/api/system/role';

import { computed, h, onMounted, reactive, ref } from 'vue';

import { Page } from '@vben/common-ui';

import {
  NButton,
  NCard,
  NDataTable,
  NDrawer,
  NDrawerContent,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NModal,
  NSelect,
  NSpace,
  NTag,
  NTree,
  useMessage,
} from 'naive-ui';

import { getMenuTreeApi } from '#/api/system/menu';
import {
  assignRoleMenusApi,
  createRoleApi,
  deleteRoleApi,
  getRoleListApi,
  getRoleMenuIdsApi,
  updateRoleApi,
} from '#/api/system/role';

const message = useMessage();

const loading = ref(false);
const rows = ref<SystemRoleApi.Role[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(10);

const query = reactive<{ keyword: string; status: null | number }>({
  keyword: '',
  status: null,
});

const statusOptions = [
  { label: '启用', value: 1 },
  { label: '禁用', value: 0 },
];

async function loadData() {
  loading.value = true;
  try {
    const result = await getRoleListApi({
      keyword: query.keyword || undefined,
      page: page.value,
      pageSize: pageSize.value,
      status: query.status ?? undefined,
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
  query.status = null;
  handleSearch();
}

/* ----------------------------- 新增 / 编辑 ----------------------------- */

interface RoleFormState {
  code: string;
  description: string;
  id: null | number;
  name: string;
  sort: number;
  status: number;
}

const formRef = ref<FormInst | null>(null);
const formVisible = ref(false);
const submitting = ref(false);
const isEdit = ref(false);
const formState = reactive<RoleFormState>({
  code: '',
  description: '',
  id: null,
  name: '',
  sort: 0,
  status: 1,
});

const rules: FormRules = {
  code: [
    { required: true, message: '请输入角色标识', trigger: ['blur', 'input'] },
    {
      pattern: /^[a-z][a-z0-9_]{1,63}$/,
      message: '角色标识由小写字母开头，可包含小写字母、数字和下划线',
      trigger: ['blur', 'input'],
    },
  ],
  name: [{ required: true, message: '请输入角色名称', trigger: ['blur', 'input'] }],
};

function openCreate() {
  isEdit.value = false;
  formState.id = null;
  formState.code = '';
  formState.name = '';
  formState.description = '';
  formState.sort = 0;
  formState.status = 1;
  formVisible.value = true;
}

function openEdit(row: SystemRoleApi.Role) {
  isEdit.value = true;
  formState.id = row.id;
  formState.code = row.code;
  formState.name = row.name;
  formState.description = row.description;
  formState.sort = row.sort;
  formState.status = row.status;
  formVisible.value = true;
}

async function handleSubmit() {
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }
  submitting.value = true;
  try {
    const payload = {
      code: formState.code,
      description: formState.description,
      name: formState.name,
      sort: formState.sort,
      status: formState.status,
    };
    if (isEdit.value && formState.id) {
      await updateRoleApi(formState.id, payload);
      message.success('角色已更新');
    } else {
      await createRoleApi(payload);
      message.success('角色创建成功');
    }
    formVisible.value = false;
    await loadData();
  } finally {
    submitting.value = false;
  }
}

async function handleDelete(row: SystemRoleApi.Role) {
  await deleteRoleApi(row.id);
  message.success('角色已删除');
  await loadData();
}

/* ------------------------------- 分配权限 ------------------------------- */

const menuDrawerVisible = ref(false);
const menuSubmitting = ref(false);
const menuTree = ref<SystemMenuApi.Menu[]>([]);
const selectedMenuIds = ref<number[]>([]);
const assignTarget = ref<null | SystemRoleApi.Role>(null);

function toTreeOptions(list: SystemMenuApi.Menu[]): TreeOption[] {
  return list.map((item) => ({
    key: item.id,
    label: `${item.title} (${item.type === 'button' ? '按钮' : item.path || item.name})`,
    children: item.children?.length ? toTreeOptions(item.children) : undefined,
  }));
}

const menuTreeOptions = computed<TreeOption[]>(() => toTreeOptions(menuTree.value));

/**
 * naive-ui 的 cascade 模式会把「父节点被勾选」理解为「其全部子节点被勾选」，
 * 因此回显时只下发最末级的已授权节点，父级由组件自行推导，避免越权显示。
 */
function toDisplayKeys(nodes: SystemMenuApi.Menu[], granted: Set<number>): number[] {
  const keys: number[] = [];
  const visit = (node: SystemMenuApi.Menu): boolean => {
    let childGranted = false;
    for (const child of node.children ?? []) {
      if (visit(child)) {
        childGranted = true;
      }
    }
    const selfGranted = granted.has(node.id);
    if (selfGranted && !childGranted) {
      keys.push(node.id);
    }
    return selfGranted || childGranted;
  };
  for (const node of nodes) {
    visit(node);
  }
  return keys;
}

/** 保存时补齐祖先节点，后端按 id 集合构建菜单树，缺少父级会导致子菜单无法挂载。 */
function withAncestors(keys: number[]): number[] {
  const parentOf = new Map<number, number>();
  const walk = (nodes: SystemMenuApi.Menu[]) => {
    for (const node of nodes) {
      for (const child of node.children ?? []) {
        parentOf.set(child.id, node.id);
      }
      walk(node.children ?? []);
    }
  };
  walk(menuTree.value);

  const result = new Set(keys);
  for (const key of keys) {
    let parent = parentOf.get(key);
    while (parent !== undefined) {
      result.add(parent);
      parent = parentOf.get(parent);
    }
  }
  return [...result].sort((a, b) => a - b);
}

async function openAssignMenus(row: SystemRoleApi.Role) {
  assignTarget.value = row;
  const [tree, ids] = await Promise.all([
    getMenuTreeApi(),
    getRoleMenuIdsApi(row.id),
  ]);
  menuTree.value = tree;
  selectedMenuIds.value = toDisplayKeys(tree, new Set(ids));
  menuDrawerVisible.value = true;
}

async function handleAssignMenus() {
  if (!assignTarget.value) {
    return;
  }
  menuSubmitting.value = true;
  try {
    await assignRoleMenusApi(assignTarget.value.id, withAncestors(selectedMenuIds.value));
    message.success('权限已保存');
    menuDrawerVisible.value = false;
  } finally {
    menuSubmitting.value = false;
  }
}

/* -------------------------------- 表格列 -------------------------------- */

const columns: DataTableColumns<SystemRoleApi.Role> = [
  { key: 'id', title: 'ID', width: 70 },
  { key: 'code', title: '角色标识', minWidth: 140 },
  { key: 'name', title: '角色名称', minWidth: 140 },
  { key: 'description', title: '描述', minWidth: 180 },
  { key: 'sort', title: '排序', width: 80 },
  {
    key: 'userCount',
    title: '用户数',
    width: 90,
  },
  {
    key: 'status',
    title: '状态',
    width: 90,
    render: (row) =>
      h(
        NTag,
        { size: 'small', type: row.status === 1 ? 'success' : 'error' },
        { default: () => (row.status === 1 ? '启用' : '禁用') },
      ),
  },
  {
    key: 'actions',
    title: '操作',
    width: 220,
    render: (row) =>
      h(
        NSpace,
        { size: 4 },
        {
          default: () => [
            h(
              NButton,
              { size: 'small', text: true, type: 'primary', onClick: () => openEdit(row) },
              { default: () => '编辑' },
            ),
            h(
              NButton,
              {
                disabled: row.code === 'super_admin',
                size: 'small',
                text: true,
                onClick: () => openAssignMenus(row),
              },
              { default: () => '分配权限' },
            ),
            h(
              NButton,
              {
                disabled: row.builtIn,
                size: 'small',
                text: true,
                type: 'error',
                onClick: () => handleDelete(row),
              },
              { default: () => '删除' },
            ),
          ],
        },
      ),
  },
];

const pagination = computed(() => ({
  itemCount: total.value,
  page: page.value,
  pageSize: pageSize.value,
  pageSizes: [10, 20, 50],
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
  <Page description="维护角色及其可见菜单、可操作按钮" title="角色管理">
    <NCard size="small">
      <template #header>
        <NSpace>
          <NInput
            v-model:value="query.keyword"
            clearable
            placeholder="角色标识 / 名称"
            style="width: 200px"
            @keyup.enter="handleSearch"
          />
          <NSelect
            v-model:value="query.status"
            clearable
            :options="statusOptions"
            placeholder="状态"
            style="width: 120px"
          />
          <NButton type="primary" @click="handleSearch">查询</NButton>
          <NButton @click="handleReset">重置</NButton>
        </NSpace>
      </template>
      <template #header-extra>
        <NButton type="primary" @click="openCreate">新增角色</NButton>
      </template>

      <NDataTable
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="pagination"
        :remote="true"
        :row-key="(row: SystemRoleApi.Role) => row.id"
        :scroll-x="1000"
      />
    </NCard>

    <NModal
      v-model:show="formVisible"
      :title="isEdit ? '编辑角色' : '新增角色'"
      preset="card"
      style="width: 520px"
    >
      <NForm
        ref="formRef"
        :model="formState"
        :rules="rules"
        label-placement="left"
        label-width="90"
      >
        <NFormItem label="角色标识" path="code">
          <NInput
            v-model:value="formState.code"
            :disabled="isEdit"
            placeholder="例如 system_admin"
          />
        </NFormItem>
        <NFormItem label="角色名称" path="name">
          <NInput v-model:value="formState.name" placeholder="例如 系统管理员" />
        </NFormItem>
        <NFormItem label="排序" path="sort">
          <NInputNumber v-model:value="formState.sort" :min="0" />
        </NFormItem>
        <NFormItem label="状态" path="status">
          <NSelect v-model:value="formState.status" :options="statusOptions" />
        </NFormItem>
        <NFormItem label="描述" path="description">
          <NInput
            v-model:value="formState.description"
            placeholder="选填"
            type="textarea"
          />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="formVisible = false">取消</NButton>
          <NButton :loading="submitting" type="primary" @click="handleSubmit">
            保存
          </NButton>
        </NSpace>
      </template>
    </NModal>

    <NDrawer v-model:show="menuDrawerVisible" :width="420">
      <NDrawerContent :title="`分配权限 - ${assignTarget?.name ?? ''}`" closable>
        <NTree
          v-model:checked-keys="selectedMenuIds"
          :data="menuTreeOptions"
          cascade
          checkable
          default-expand-all
          selectable
        />
        <template #footer>
          <NSpace justify="end">
            <NButton @click="menuDrawerVisible = false">取消</NButton>
            <NButton :loading="menuSubmitting" type="primary" @click="handleAssignMenus">
              保存
            </NButton>
          </NSpace>
        </template>
      </NDrawerContent>
    </NDrawer>
  </Page>
</template>
