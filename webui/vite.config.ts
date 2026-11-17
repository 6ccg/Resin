import react from "@vitejs/plugin-react";
import { gzipSync } from "node:zlib";
import { defineConfig, loadEnv } from "vite";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  const apiTarget = env.VITE_DEV_API_TARGET || "http://127.0.0.1:2260";

  return {
    base: "/ui/",
    plugins: [
      react(),
      {
        name: "resin-gzip-assets",
        apply: "build",
        generateBundle(_options, bundle) {
          for (const output of Object.values(bundle)) {
            if (!output.fileName.startsWith("assets/") || !/\.(js|css|svg|json)$/.test(output.fileName)) continue;
            const source = output.type === "chunk" ? output.code : output.source;
            const gzip = gzipSync(source);
            if (gzip.length < Buffer.byteLength(source)) {
              this.emitFile({ type: "asset", fileName: `${output.fileName}.gz`, source: gzip });
            }
          }
        },
      },
    ],
    build: {
      chunkSizeWarningLimit: 1200,
    },
    server: {
      host: "0.0.0.0",
      port: 5173,
      proxy: {
        "/api": {
          target: apiTarget,
          changeOrigin: true,
        },
        "/healthz": {
          target: apiTarget,
          changeOrigin: true,
        },
      },
    },
  };
});
