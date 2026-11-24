import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import federation from '@originjs/vite-plugin-federation';
import { versionTrackerPlugin } from '../../code-common/build-utils/versionTrackerPlugin';

export default defineConfig({
  plugins: [
    react(),
    versionTrackerPlugin({ appName: 'code-markdocx' }),
    federation({
      name: 'markdocx',
      filename: 'remoteEntry.js',
      exposes: {
        './App': './src/App.tsx',
        './menu': './src/menu.ts',
      },
      shared: ['react', 'react-dom', 'react-router-dom'],
    }),
  ],
  base: process.env.VITE_BASE_PATH || '/markdocx/',
  build: {
    target: 'esnext',
    minify: 'esbuild',
    cssCodeSplit: false,
  },
  server: {
    port: 5185,
    proxy: {
      '/markdocx/api': {
        target: 'http://127.0.0.1:8105',
        changeOrigin: true,
      },
      '/api': {
        target: 'http://127.0.0.1:8105',
        changeOrigin: true,
      },
    },
  },
});
