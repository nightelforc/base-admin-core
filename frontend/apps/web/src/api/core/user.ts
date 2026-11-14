import type { UserInfo } from '@vben/types';

import { requestClient } from '#/api/request';

/** 后端 /auth/profile 返回的结构 */
export interface ProfileResult {
  avatar: string;
  email: string;
  homePath: string;
  nickname: string;
  phone: string;
  realName: string;
  roles: string[];
  userId: number;
  username: string;
}

/**
 * 获取用户信息
 *
 * 后端返回的是本系统的 profile 结构，这里转换成 vben 需要的 UserInfo，
 * 避免框架层直接依赖后端的字段命名。
 */
export async function getUserInfoApi(): Promise<UserInfo> {
  const profile = await requestClient.get<ProfileResult>('/auth/profile');

  return {
    avatar: profile.avatar,
    realName: profile.realName || profile.nickname || profile.username,
    roles: profile.roles ?? [],
    userId: String(profile.userId),
    username: profile.username,
    desc: profile.email,
    homePath: profile.homePath || '/dashboard/workspace',
    // 访问令牌保存在 accessStore 中，这里只是类型占位。
    token: '',
  };
}
