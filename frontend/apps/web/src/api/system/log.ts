import type { PageResult } from '../types';

import { requestClient } from '#/api/request';

export namespace SystemLogApi {
  export interface LoginLog {
    createdAt: string;
    id: number;
    ip: string;
    message: string;
    success: boolean;
    userAgent: string;
    userId: number;
    username: string;
  }

  export interface OperationLog {
    createdAt: string;
    id: number;
    ip: string;
    latencyMs: number;
    method: string;
    path: string;
    requestBody: string;
    statusCode: number;
    userAgent: string;
    userId: number;
    username: string;
  }

  export interface LoginQuery {
    from?: string;
    keyword?: string;
    page?: number;
    pageSize?: number;
    success?: number;
    to?: string;
    userId?: number;
  }

  export interface OperationQuery {
    from?: string;
    keyword?: string;
    page?: number;
    pageSize?: number;
    to?: string;
    userId?: number;
  }
}

export async function getLoginLogsApi(params: SystemLogApi.LoginQuery) {
  return requestClient.get<PageResult<SystemLogApi.LoginLog>>('/logs/login', {
    params,
  });
}

export async function getOperationLogsApi(
  params: SystemLogApi.OperationQuery,
) {
  return requestClient.get<PageResult<SystemLogApi.OperationLog>>(
    '/logs/operation',
    { params },
  );
}
