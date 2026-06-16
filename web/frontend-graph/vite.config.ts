import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  base: '/static/graph-app/',
  build: {
    outDir: '../static/graph-app',
    emptyOutDir: true,
  },
})
