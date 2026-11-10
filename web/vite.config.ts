import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// The production build lands in ../internal/webui/dist, which the Go binary
// embeds. In development, Vite serves the UI and proxies the API to Stim.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': process.env.STIM_API ?? 'http://127.0.0.1:7846',
    },
  },
});
