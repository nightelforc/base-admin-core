import { requestClient } from '#/api/request';

export namespace AuthApi {
  /** 登录接口参数 */
  export interface LoginParams {
    password?: string;
    username?: string;
  }

  /** 登录接口返回值 */
  export interface LoginResult {
    accessToken: string;
    expiresIn: number;
    refreshToken: string;
    tokenType: string;
  }

  /** 刷新令牌接口返回值 */
  export interface RefreshResult {
    accessToken: string;
    expiresIn: number;
    refreshToken: string;
    tokenType: string;
  }
}

/**
 * 登录
 */
export async function loginApi(data: AuthApi.LoginParams) {
  return requestClient.post<AuthApi.LoginResult>('/auth/login', data);
}

/**
 * 刷新令牌。
 *
 * 这里不使用 baseRequestClient：刷新失败时同样需要走统一的错误处理，
 * 并且刷新接口把 refreshToken 放在请求体里，而不是依赖 Cookie。
 */
export async function refreshTokenApi(refreshToken: string) {
  return requestClient.post<AuthApi.RefreshResult>(
    '/auth/refresh',
    { refreshToken },
    { responseReturn: 'data' },
  );
}

/**
 * 退出登录
 */
export async function logoutApi() {
  return requestClient.post('/auth/logout');
}

/**
 * 获取用户权限码
 */
export async function getAccessCodesApi() {
  return requestClient.get<string[]>('/auth/codes');
}
