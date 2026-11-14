import type { RouteRecordStringComponent } from '@vben/types';

import { requestClient } from '#/api/request';

/**
 * 获取当前用户的菜单与路由
 *
 * 后端按数据库中的菜单表生成 vben 可识别的路由记录，
 * component 字段形如 `/system/user/index`，由前端映射到 views 下的页面。
 */
export async function getAllMenusApi() {
  return requestClient.get<RouteRecordStringComponent[]>('/auth/menus');
}
