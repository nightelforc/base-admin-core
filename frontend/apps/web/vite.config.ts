import { defineConfig } from '@vben/vite-config';

export default defineConfig(async () => {
  return {
    application: {},
    vite: {
      server: {
        proxy: {
          // Backend API (Go / gin) — keep the /api prefix, it is part of the API contract.
          '/api': {
            changeOrigin: true,
            target: 'http://127.0.0.1:8080',
            ws: true,
          },
        },
      },
    },
  };
});
