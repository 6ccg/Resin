import react from "@vitejs/plugin-react";
import { readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
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
        async writeBundle(options, bundle) {
          if (!options.dir) return;
          for (const name of Object.keys(bundle)) {
            if (!name.startsWith("assets/") || !/\.(js|css|svg|json)$/.test(name)) continue;
            // Vite can rewrite generated assets before writing them to disk.
            const file = join(options.dir, name);
            const source = await readFile(file);
            const gzip = gzipSync(source);
            if (gzip.length < source.length) {
              await writeFile(file + ".gz", gzip);
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
