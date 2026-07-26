import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

const gatewayTarget = process.env.VITE_DEV_GATEWAY_TARGET || 'http://localhost:18080';

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5176,
    open: false,
    proxy: {
      '/graphql': {
        target: gatewayTarget,
        changeOrigin: true,
        secure: false
      }
    }
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: false
  }
});
