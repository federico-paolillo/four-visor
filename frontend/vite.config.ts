import preact from "@preact/preset-vite";
import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [preact(), tailwindcss()],
  test: {
    passWithNoTests: true,
  },
  server: {
    proxy: {
      '/api': {
        target: 'http://localhost:65102',
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/api/, ''),
      },
    }
  },
});
