import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwind from "@tailwindcss/vite";
import { TanStackRouterVite } from "@tanstack/router-plugin/vite";
import path from "node:path";

// Vite config for the web/client/ subapp. The dev server proxies /api to the
// Hono server (which runs on PORT=3333 by default). `npm run build` writes to
// web/dist/ so the Hono server can serve it as static files in production.
export default defineConfig({
  root: path.resolve(__dirname),
  build: {
    outDir: path.resolve(__dirname, "..", "dist"),
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      "/api": "http://127.0.0.1:3333",
    },
  },
  plugins: [
    TanStackRouterVite({
      routesDirectory: path.resolve(__dirname, "src", "routes"),
      generatedRouteTree: path.resolve(__dirname, "src", "routeTree.gen.ts"),
    }),
    react(),
    tailwind(),
  ],
});
