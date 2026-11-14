import { defineOverridesPreferences } from '@vben/preferences';

/**
 * @description 项目配置文件
 * 只需要覆盖项目中的一部分配置，不需要的配置不用覆盖，会自动使用默认配置
 * !!! 更改配置后请清空缓存，否则可能不生效
 */
export const overridesPreferences = defineOverridesPreferences({
  // overrides
  app: {
    name: import.meta.env.VITE_APP_TITLE,
    // 菜单与路由由后端下发（GET /api/v1/auth/menus）
    accessMode: 'backend',
    defaultHomePath: '/dashboard/workspace',
    // 使用 accessToken + refreshToken 双令牌，静默续期
    enableRefreshToken: true,
    // 令牌彻底失效时跳转登录页，而不是弹窗
    loginExpiredMode: 'page',
  },
  logo: {
    // 使用 apps/web/public/logo.svg，避免依赖外部 CDN
    source: '/logo.svg',
  },
  copyright: {
    companyName: 'Base Admin',
    companySiteLink: '',
    date: '2026',
  },
});
