import type { PageResult } from '../types';

import { requestClient } from '#/api/request';

export namespace SystemRoleApi {
  export interface Role {
    builtIn: boolean;
    code: string;
    createdAt: string;
    description: string;
    id: number;
    name: string;
    sort: number;
    status: number;
    userCount: number;
  }

  export interface Option {
    code: string;
    id: number;
    name: string;
  }

  export interface Query {
    keyword?: string;
    page?: number;
    pageSize?: number;
    status?: number;
  }

  export interface Params {
    code: string;
    description?: string;
    name: string;
    sort?: number;
    status?: number;
  }
}

export async function getRoleListApi(params: SystemRoleApi.Query) {
  return requestClient.get<PageResult<SystemRoleApi.Role>>('/roles', {
    params,
  });
}

export async function getRoleOptionsApi() {
  return requestClient.get<SystemRoleApi.Option[]>('/roles/options');
}

export async function createRoleApi(data: SystemRoleApi.Params) {
  return requestClient.post<{ id: number }>('/roles', data);
}

export async function updateRoleApi(id: number, data: SystemRoleApi.Params) {
  return requestClient.put(`/roles/${id}`, data);
}

export async function deleteRoleApi(id: number) {
  return requestClient.delete(`/roles/${id}`);
}

export async function getRoleMenuIdsApi(id: number) {
  return requestClient.get<number[]>(`/roles/${id}/menus`);
}

export async function assignRoleMenusApi(id: number, menuIds: number[]) {
  return requestClient.put(`/roles/${id}/menus`, { menuIds });
}
