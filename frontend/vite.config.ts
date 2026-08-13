import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Phase 1 POC: nginx serves this build's dist/ output as static files and
// reverse-proxies /api + /auth through to the Go backend on the same box.
// See /infrastructure for the VM/nginx setup. In local dev, the proxy below sends
// those same paths to a backend running on :8080.
export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      "/api": "http://localhost:8080",
      "/auth": "http://localhost:8080",
    },
  },
});
