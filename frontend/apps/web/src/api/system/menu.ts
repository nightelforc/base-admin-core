import { requestClient } from '#/api/request';

export namespace SystemMenuApi {
  export type MenuType =
    | 'button'
    | 'catalog'
    | 'embedded'
    | 'link'
    | 'menu';

  export interface Menu {
    affixTab: boolean;
    children?: Menu[];
    component: string;
    createdAt: string;
    icon: string;
    id: number;
    keepAlive: boolean;
    link: string;
    name: string;
    parentId: number;
    path: string;
    permission: string;
    redirect: string;
    sort: number;
    status: number;
    title: string;
    type: MenuType;
    visible: boolean;
  }

  export interface Params {
    affixTab?: boolean;
    component?: string;
    icon?: string;
    keepAlive?: boolean;
    link?: string;
    name: string;
    parentId?: number;
    path?: string;
    permission?: string;
    redirect?: string;
    sort?: number;
    status?: number;
    title: string;
    type: MenuType;
    visible?: boolean;
  }
}

export async function getMenuTreeApi() {
  return requestClient.get<SystemMenuApi.Menu[]>('/menus/tree');
}

export async function createMenuApi(data: SystemMenuApi.Params) {
  return requestClient.post<{ id: number }>('/menus', data);
}

export async function updateMenuApi(id: number, data: SystemMenuApi.Params) {
  return requestClient.put(`/menus/${id}`, data);
}

export async function deleteMenuApi(id: number) {
  return requestClient.delete(`/menus/${id}`);
}
