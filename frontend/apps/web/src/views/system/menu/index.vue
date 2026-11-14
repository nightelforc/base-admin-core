<script setup lang="ts">
import type {
  DataTableColumns,
  FormInst,
  FormRules,
  TreeSelectOption,
} from 'naive-ui';

import type { SystemMenuApi } from '#/api/system/menu';

import { computed, h, onMounted, ref } from 'vue';

import { Page } from '@vben/common-ui';

import {
  NButton,
  NCard,
  NCheckbox,
  NDataTable,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NModal,
  NSelect,
  NSpace,
  NTag,
  NTreeSelect,
  useMessage,
} from 'naive-ui';

import {
  createMenuApi,
  deleteMenuApi,
  getMenuTreeApi,
  updateMenuApi,
} from '#/api/system/menu';

const message = useMessage();

const loading = ref(false);
const tree = ref<SystemMenuApi.Menu[]>([]);

const typeOptions = [
  { label: '目录', value: 'catalog' },
  { label: '菜单', value: 'menu' },
  { label: '按钮', value: 'button' },
  { label: '内嵌', value: 'embedded' },
  { label: '外链', value: 'link' },
];

const statusOptions = [
  { label: '启用', value: 1 },
  { label: '禁用', value: 0 },
];

const typeMeta: Record<
  string,
  { text: string; type: 'default' | 'error' | 'info' | 'success' | 'warning' }
> = {
  button: { text: '按钮', type: 'warning' },
  catalog: { text: '目录', type: 'info' },
  embedded: { text: '内嵌', type: 'default' },
  link: { text: '外链', type: 'default' },
  menu: { text: '菜单', type: 'success' },
};

async function loadData() {
  loading.value = true;
  try {
    tree.value = await getMenuTreeApi();
  } finally {
    loading.value = false;
  }
}

/* ---------------------------- 父级选择数据源 ---------------------------- */

function toParentOptions(
  list: SystemMenuApi.Menu[],
  excludeId: null | number,
): TreeSelectOption[] {
  const options: TreeSelectOption[] = [];
  for (const item of list) {
    if (item.type === 'button' || item.id === excludeId) {
      continue;
    }
    options.push({
      key: item.id,
      label: item.title,
      children: item.children?.length
        ? toParentOptions(item.children, excludeId)
        : undefined,
    });
  }
  return options;
}

/* ----------------------------- 新增 / 编辑 ----------------------------- */

interface MenuFormState {
  affixTab: boolean;
  component: string;
  icon: string;
  id: null | number;
  keepAlive: boolean;
  link: string;
  name: string;
  parentId: null | number;
  path: string;
  permission: string;
  redirect: string;
  sort: number;
  status: number;
  title: string;
  type: string;
  visible: boolean;
}

function emptyForm(): MenuFormState {
  return {
    affixTab: false,
    component: '',
    icon: '',
    id: null,
    keepAlive: false,
    link: '',
    name: '',
    parentId: null,
    path: '',
    permission: '',
    redirect: '',
    sort: 0,
    status: 1,
    title: '',
    type: 'menu',
    visible: true,
  };
}

const formRef = ref<FormInst | null>(null);
const formVisible = ref(false);
const submitting = ref(false);
const isEdit = ref(false);
const form = ref<MenuFormState>(emptyForm());

const parentOptions = computed(() => toParentOptions(tree.value, form.value.id));

const rules: FormRules = {
  name: [
    { required: true, message: '请输入路由名称', trigger: ['blur', 'input'] },
    {
      pattern: /^[A-Za-z][A-Za-z0-9_-]{1,63}$/,
      message: '路由名称需以字母开头，可包含字母、数字、下划线和短横线',
      trigger: ['blur', 'input'],
    },
  ],
  title: [
    { required: true, message: '请输入菜单标题', trigger: ['blur', 'input'] },
  ],
};

function openCreate(parent?: SystemMenuApi.Menu) {
  isEdit.value = false;
  form.value = emptyForm();
  if (parent) {
    form.value.parentId = parent.id;
    form.value.type = parent.type === 'catalog' ? 'menu' : 'button';
  }
  formVisible.value = true;
}

function openEdit(row: SystemMenuApi.Menu) {
  isEdit.value = true;
  form.value = {
    affixTab: row.affixTab,
    component: row.component,
    icon: row.icon,
    id: row.id,
    keepAlive: row.keepAlive,
    link: row.link,
    name: row.name,
    parentId: row.parentId === 0 ? null : row.parentId,
    path: row.path,
    permission: row.permission,
    redirect: row.redirect,
    sort: row.sort,
    status: row.status,
    title: row.title,
    type: row.type,
    visible: row.visible,
  };
  formVisible.value = true;
}

async function handleSubmit() {
  try {
    await formRef.value?.validate();
  } catch {
    return;
  }

  const state = form.value;
  const payload: SystemMenuApi.Params = {
    affixTab: state.affixTab,
    component: state.component,
    icon: state.icon,
    keepAlive: state.keepAlive,
    link: state.link,
    name: state.name,
    parentId: state.parentId ?? 0,
    path: state.path,
    permission: state.permission,
    redirect: state.redirect,
    sort: state.sort,
    status: state.status,
    title: state.title,
    type: state.type as SystemMenuApi.MenuType,
    visible: state.visible,
  };

  submitting.value = true;
  try {
    if (isEdit.value && state.id) {
      await updateMenuApi(state.id, payload);
      message.success('菜单已更新');
    } else {
      await createMenuApi(payload);
      message.success('菜单创建成功');
    }
    formVisible.value = false;
    await loadData();
  } finally {
    submitting.value = false;
  }
}

async function handleDelete(row: SystemMenuApi.Menu) {
  await deleteMenuApi(row.id);
  message.success('菜单已删除');
  await loadData();
}

/* -------------------------------- 表格列 -------------------------------- */

const columns: DataTableColumns<SystemMenuApi.Menu> = [
  { key: 'title', title: '标题', minWidth: 200 },
  {
    key: 'type',
    title: '类型',
    width: 90,
    render: (row) => {
      const meta = typeMeta[row.type] ?? {
        text: row.type,
        type: 'default' as const,
      };
      return h(
        NTag,
        { size: 'small', type: meta.type },
        { default: () => meta.text },
      );
    },
  },
  { key: 'name', title: '路由名称', minWidth: 150 },
  { key: 'path', title: '路径', minWidth: 150 },
  { key: 'component', title: '组件', minWidth: 180 },
  { key: 'permission', title: '权限码', minWidth: 180 },
  { key: 'sort', title: '排序', width: 80 },
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
    width: 210,
    render: (row) =>
      h(
        NSpace,
        { size: 4 },
        {
          default: () => [
            h(
              NButton,
              {
                size: 'small',
                text: true,
                type: 'primary',
                onClick: () => openEdit(row),
              },
              { default: () => '编辑' },
            ),
            h(
              NButton,
              {
                disabled: row.type !== 'catalog' && row.type !== 'menu',
                size: 'small',
                text: true,
                onClick: () => openCreate(row),
              },
              { default: () => '新增子项' },
            ),
            h(
              NButton,
              {
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

onMounted(loadData);
</script>

<template>
  <Page description="菜单同时决定侧边栏结构与按钮级权限码" title="菜单管理">
    <NCard size="small">
      <template #header>
        <span class="text-gray-500">按树形结构维护目录、菜单与按钮权限</span>
      </template>
      <template #header-extra>
        <NSpace>
          <NButton @click="loadData">刷新</NButton>
          <NButton type="primary" @click="openCreate()">新增菜单</NButton>
        </NSpace>
      </template>

      <NDataTable
        :columns="columns"
        :data="tree"
        :loading="loading"
        :row-key="(row: SystemMenuApi.Menu) => row.id"
        :scroll-x="1200"
        default-expand-all
      />
    </NCard>

    <NModal
      v-model:show="formVisible"
      :title="isEdit ? '编辑菜单' : '新增菜单'"
      preset="card"
      style="width: 620px"
    >
      <NForm
        ref="formRef"
        :model="form"
        :rules="rules"
        label-placement="left"
        label-width="100"
      >
        <NFormItem label="上级菜单" path="parentId">
          <NTreeSelect
            v-model:value="form.parentId"
            clearable
            :options="parentOptions"
            placeholder="不选表示顶级菜单"
          />
        </NFormItem>
        <NFormItem label="类型" path="type">
          <NSelect v-model:value="form.type" :options="typeOptions" />
        </NFormItem>
        <NFormItem label="标题" path="title">
          <NInput v-model:value="form.title" placeholder="显示在菜单/面包屑上的名称" />
        </NFormItem>
        <NFormItem label="路由名称" path="name">
          <NInput
            v-model:value="form.name"
            placeholder="全局唯一的英文名称，例如 SystemUser"
          />
        </NFormItem>
        <NFormItem label="访问路径" path="path">
          <NInput v-model:value="form.path" placeholder="以 / 开头，例如 /system/user" />
        </NFormItem>
        <NFormItem label="页面组件" path="component">
          <NInput
            v-model:value="form.component"
            placeholder="相对 views 的路径，例如 /system/user/index"
          />
        </NFormItem>
        <NFormItem label="重定向" path="redirect">
          <NInput v-model:value="form.redirect" placeholder="选填，默认取第一个子菜单" />
        </NFormItem>
        <NFormItem label="外链地址" path="link">
          <NInput v-model:value="form.link" placeholder="https://example.com" />
        </NFormItem>
        <NFormItem label="权限码" path="permission">
          <NInput
            v-model:value="form.permission"
            placeholder="按钮类菜单必填，例如 system:user:create"
          />
        </NFormItem>
        <NFormItem label="图标" path="icon">
          <NInput v-model:value="form.icon" placeholder="例如 lucide:users" />
        </NFormItem>
        <NFormItem label="排序" path="sort">
          <NInputNumber v-model:value="form.sort" :min="0" />
        </NFormItem>
        <NFormItem label="状态" path="status">
          <NSelect v-model:value="form.status" :options="statusOptions" />
        </NFormItem>
        <NFormItem label="显示与缓存">
          <NSpace>
            <NCheckbox v-model:checked="form.visible">显示在菜单</NCheckbox>
            <NCheckbox v-model:checked="form.keepAlive">页面缓存</NCheckbox>
            <NCheckbox v-model:checked="form.affixTab">固定标签页</NCheckbox>
          </NSpace>
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
  </Page>
</template>
