<script setup lang="ts">
import type { DataTableColumns, FormInst, FormRules } from 'naive-ui';

import type { SystemRoleApi } from '#/api/system/role';
import type { SystemUserApi } from '#/api/system/user';

import { computed, h, onMounted, reactive, ref } from 'vue';

import { Page } from '@vben/common-ui';

import {
  NButton,
  NCard,
  NDataTable,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NSelect,
  NSpace,
  NTag,
  useMessage,
} from 'naive-ui';

import {
  createUserApi,
  deleteUserApi,
  getUserListApi,
  resetUserPasswordApi,
  updateUserApi,
  updateUserStatusApi,
} from '#/api/system/user';
import { getRoleOptionsApi } from '#/api/system/role';

const message = useMessage();

const loading = ref(false);
const rows = ref<SystemUserApi.User[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(10);

const roleOptions = ref<SystemRoleApi.Option[]>([]);

const query = reactive<{ keyword: string; roleId: null | number; status: null | number }>({
  keyword: '',
  roleId: null,
  status: null,
});

const statusOptions = [
  { label: '启用', value: 1 },
  { label: '禁用', value: 0 },
];

const roleSelectOptions = ref<{ label: string; value: number }[]>([]);

async function loadData() {
  loading.value = true;
  try {
    const result = await getUserListApi({
      keyword: query.keyword || undefined,
      page: page.value,
      pageSize: pageSize.value,
      roleId: query.roleId ?? undefined,
      status: query.status ?? undefined,
    });
    rows.value = result.list;
    total.value = result.total;
  } finally {
    loading.value = false;
  }
}

async function loadRoles() {
  roleOptions.value = await getRoleOptionsApi();
  roleSelectOptions.value = roleOptions.value.map((item) => ({
    label: `${item.name} (${item.code})`,
    value: item.id,
  }));
}

function handleSearch() {
  page.value = 1;
  loadData();
}

function handleReset() {
  query.keyword = '';
  query.roleId = null;
  query.status = null;
  handleSearch();
}

/* ----------------------------- 新增 / 编辑 ----------------------------- */

interface UserFormState {
  email: string;
  id: null | number;
  nickname: string;
  password: string;
  phone: string;
  remark: string;
  roleIds: number[];
  status: number;
  username: string;
}

const formRef = ref<FormInst | null>(null);
const formVisible = ref(false);
const submitting = ref(false);
const formState = reactive<UserFormState>({
  email: '',
  id: null,
  nickname: '',
  password: '',
  phone: '',
  remark: '',
  roleIds: [],
  status: 1,
  username: '',
});

const isEdit = ref(false);

const rules: FormRules = {
  username: [
    { required: true, message: '请输入用户名', trigger: ['blur', 'input'] },
    {
      pattern: /^[a-zA-Z0-9_.-]{3,32}$/,
      message: '用户名由 3-32 位字母、数字、下划线、点或短横线组成',
      trigger: ['blur', 'input'],
    },
  ],
  password: [
    {
      required: true,
      message: '请输入登录密码',
      trigger: ['blur', 'input'],
    },
    {
      pattern: /^(?=.*[a-zA-Z])(?=.*\d).{8,64}$/,
      message: '密码至少 8 位，且同时包含字母和数字',
      trigger: ['blur', 'input'],
    },
  ],
};

function resetForm() {
  formState.id = null;
  formState.username = '';
  formState.password = '';
  formState.nickname = '';
  formState.email = '';
  formState.phone = '';
  formState.remark = '';
  formState.roleIds = [];
  formState.status = 1;
}

function openCreate() {
  isEdit.value = false;
  resetForm();
  formVisible.value = true;
}

function openEdit(row: SystemUserApi.User) {
  isEdit.value = true;
  formState.id = row.id;
  formState.username = row.username;
  formState.password = '';
  formState.nickname = row.nickname;
  formState.email = row.email;
  formState.phone = row.phone;
  formState.remark = row.remark;
  formState.roleIds = [...row.roleIds];
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
    if (isEdit.value && formState.id) {
      await updateUserApi(formState.id, {
        email: formState.email,
        nickname: formState.nickname,
        phone: formState.phone,
        remark: formState.remark,
        roleIds: formState.roleIds,
        status: formState.status,
      });
      message.success('用户信息已更新');
    } else {
      await createUserApi({
        email: formState.email,
        nickname: formState.nickname,
        password: formState.password,
        phone: formState.phone,
        remark: formState.remark,
        roleIds: formState.roleIds,
        status: formState.status,
        username: formState.username,
      });
      message.success('用户创建成功');
    }
    formVisible.value = false;
    await loadData();
  } finally {
    submitting.value = false;
  }
}

/* ------------------------------ 重置密码 ------------------------------ */

const passwordVisible = ref(false);
const passwordTarget = ref<null | SystemUserApi.User>(null);
const passwordValue = ref('');

function openResetPassword(row: SystemUserApi.User) {
  passwordTarget.value = row;
  passwordValue.value = '';
  passwordVisible.value = true;
}

async function handleResetPassword() {
  if (!passwordTarget.value) {
    return;
  }
  if (!/^(?=.*[a-zA-Z])(?=.*\d).{8,64}$/.test(passwordValue.value)) {
    message.warning('密码至少 8 位，且同时包含字母和数字');
    return;
  }
  await resetUserPasswordApi(passwordTarget.value.id, passwordValue.value);
  message.success('密码已重置，该用户需要重新登录');
  passwordVisible.value = false;
}

/* ------------------------------ 状态 / 删除 ------------------------------ */

async function handleToggleStatus(row: SystemUserApi.User) {
  const next = row.status === 1 ? 0 : 1;
  await updateUserStatusApi(row.id, next);
  row.status = next;
  message.success(next === 1 ? '用户已启用' : '用户已禁用');
}

async function handleDelete(row: SystemUserApi.User) {
  await deleteUserApi(row.id);
  message.success('用户已删除');
  if (rows.value.length === 1 && page.value > 1) {
    page.value -= 1;
  }
  await loadData();
}

/* -------------------------------- 表格列 -------------------------------- */

const columns: DataTableColumns<SystemUserApi.User> = [
  { key: 'id', title: 'ID', width: 70 },
  { key: 'username', title: '用户名', minWidth: 120 },
  { key: 'nickname', title: '昵称', minWidth: 120 },
  {
    key: 'roleNames',
    title: '角色',
    minWidth: 160,
    render: (row) =>
      h(
        NSpace,
        { size: 4 },
        {
          default: () =>
            row.roleNames.map((name, index) =>
              h(
                NTag,
                { key: row.roleIds[index] ?? name, size: 'small', type: 'info' },
                { default: () => name },
              ),
            ),
        },
      ),
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
    key: 'lastLoginAt',
    title: '最后登录',
    minWidth: 170,
    render: (row) => row.lastLoginAt?.replace('T', ' ').slice(0, 19) ?? '-',
  },
  {
    key: 'actions',
    title: '操作',
    width: 260,
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
              { size: 'small', text: true, onClick: () => openResetPassword(row) },
              { default: () => '重置密码' },
            ),
            h(
              NButton,
              { size: 'small', text: true, onClick: () => handleToggleStatus(row) },
              { default: () => (row.status === 1 ? '禁用' : '启用') },
            ),
            h(
              NButton,
              { size: 'small', text: true, type: 'error', onClick: () => handleDelete(row) },
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

onMounted(async () => {
  await Promise.all([loadRoles(), loadData()]);
});
</script>

<template>
  <Page description="维护后台用户及其角色归属" title="用户管理">
    <NCard size="small">
      <template #header>
        <NSpace>
          <NInput
            v-model:value="query.keyword"
            clearable
            placeholder="用户名 / 昵称 / 邮箱"
            style="width: 220px"
            @keyup.enter="handleSearch"
          />
          <NSelect
            v-model:value="query.status"
            clearable
            :options="statusOptions"
            placeholder="状态"
            style="width: 120px"
          />
          <NSelect
            v-model:value="query.roleId"
            clearable
            :options="roleSelectOptions"
            placeholder="角色"
            style="width: 200px"
          />
          <NButton type="primary" @click="handleSearch">查询</NButton>
          <NButton @click="handleReset">重置</NButton>
        </NSpace>
      </template>
      <template #header-extra>
        <NButton type="primary" @click="openCreate">新增用户</NButton>
      </template>

      <NDataTable
        :columns="columns"
        :data="rows"
        :loading="loading"
        :pagination="pagination"
        :remote="true"
        :row-key="(row: SystemUserApi.User) => row.id"
        :scroll-x="1100"
      />
    </NCard>

    <NModal
      v-model:show="formVisible"
      :title="isEdit ? '编辑用户' : '新增用户'"
      preset="card"
      style="width: 560px"
    >
      <NForm
        ref="formRef"
        :model="formState"
        :rules="isEdit ? { ...rules, password: [] } : rules"
        label-placement="left"
        label-width="90"
      >
        <NFormItem label="用户名" path="username">
          <NInput
            v-model:value="formState.username"
            :disabled="isEdit"
            placeholder="登录账号，创建后不可修改"
          />
        </NFormItem>
        <NFormItem v-if="!isEdit" label="登录密码" path="password">
          <NInput
            v-model:value="formState.password"
            placeholder="至少 8 位，包含字母和数字"
            type="password"
          />
        </NFormItem>
        <NFormItem label="昵称" path="nickname">
          <NInput v-model:value="formState.nickname" placeholder="显示名称" />
        </NFormItem>
        <NFormItem label="邮箱" path="email">
          <NInput v-model:value="formState.email" placeholder="选填" />
        </NFormItem>
        <NFormItem label="手机号" path="phone">
          <NInput v-model:value="formState.phone" placeholder="选填" />
        </NFormItem>
        <NFormItem label="角色" path="roleIds">
          <NSelect
            v-model:value="formState.roleIds"
            :options="roleSelectOptions"
            multiple
            placeholder="可分配多个角色"
          />
        </NFormItem>
        <NFormItem label="状态" path="status">
          <NSelect v-model:value="formState.status" :options="statusOptions" />
        </NFormItem>
        <NFormItem label="备注" path="remark">
          <NInput
            v-model:value="formState.remark"
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

    <NModal
      v-model:show="passwordVisible"
      preset="card"
      style="width: 420px"
      title="重置密码"
    >
      <NForm label-placement="left" label-width="90">
        <NFormItem label="用户名">
          <span>{{ passwordTarget?.username }}</span>
        </NFormItem>
        <NFormItem label="新密码">
          <NInput
            v-model:value="passwordValue"
            placeholder="至少 8 位，包含字母和数字"
            type="password"
          />
        </NFormItem>
      </NForm>
      <template #footer>
        <NSpace justify="end">
          <NButton @click="passwordVisible = false">取消</NButton>
          <NButton type="primary" @click="handleResetPassword">确定</NButton>
        </NSpace>
      </template>
    </NModal>
  </Page>
</template>
