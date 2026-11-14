import type { PageResult } from '../types';

import { requestClient } from '#/api/request';

export namespace SystemUserApi {
  export interface User {
    avatar: string;
    createdAt: string;
    email: string;
    id: number;
    lastLoginAt: null | string;
    lastLoginIp: string;
    nickname: string;
    phone: string;
    remark: string;
    roleCodes: string[];
    roleIds: number[];
    roleNames: string[];
    status: number;
    username: string;
  }

  export interface Query {
    keyword?: string;
    page?: number;
    pageSize?: number;
    roleId?: number;
    status?: number;
  }

  export interface CreateParams {
    email?: string;
    nickname?: string;
    password: string;
    phone?: string;
    remark?: string;
    roleIds?: number[];
    status?: number;
    username: string;
  }

  export interface UpdateParams {
    email?: string;
    nickname?: string;
    phone?: string;
    remark?: string;
    roleIds?: number[];
    status?: number;
  }
}

export async function getUserListApi(params: SystemUserApi.Query) {
  return requestClient.get<PageResult<SystemUserApi.User>>('/users', {
    params,
  });
}

export async function createUserApi(data: SystemUserApi.CreateParams) {
  return requestClient.post<{ id: number }>('/users', data);
}

export async function updateUserApi(
  id: number,
  data: SystemUserApi.UpdateParams,
) {
  return requestClient.put(`/users/${id}`, data);
}

export async function deleteUserApi(id: number) {
  return requestClient.delete(`/users/${id}`);
}

export async function resetUserPasswordApi(id: number, password: string) {
  return requestClient.put(`/users/${id}/password`, { password });
}

export async function updateUserStatusApi(id: number, status: number) {
  return requestClient.put(`/users/${id}/status`, { status });
}
