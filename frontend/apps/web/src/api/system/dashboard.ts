import { requestClient } from '#/api/request';

export namespace DashboardApi {
  export interface Trend {
    date: string;
    failed: number;
    success: number;
    total: number;
  }

  export interface Overview {
    generatedAt: string;
    loginFailedToday: number;
    loginToday: number;
    menuTotal: number;
    operationToday: number;
    roleTotal: number;
    trend: Trend[];
    userEnabled: number;
    userTotal: number;
  }
}

export async function getDashboardOverviewApi() {
  return requestClient.get<DashboardApi.Overview>('/dashboard/overview');
}
