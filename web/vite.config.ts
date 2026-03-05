import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

const isWcBuild = process.env.BUILD_WC === 'true'

export default defineConfig({
  plugins: [
    vue({
      customElement: isWcBuild,
    }),
  ],
  server: {
    proxy: {
      '/ws': {
        target: 'http://localhost:8080',
        ws: true,
      },
      '/api': {
        target: 'http://localhost:8080',
      },
    },
  },
  build: isWcBuild
    ? {
        outDir: 'dist-wc',
        emptyOutDir: true,
        lib: {
          entry: 'src/main-wc.ts',
          name: 'FlumeViewer',
          fileName: 'flume-viewer',
          formats: ['es', 'umd'],
        },
        rollupOptions: {
          // Vue is bundled into the output so the custom element is fully self-contained
          external: [],
        },
      }
    : {
        outDir: 'dist',
        emptyOutDir: true,
      },
})
