import { defineConfig } from "vite";
import { fileURLToPath } from "node:url";
export default defineConfig({
  build: {
    outDir: fileURLToPath(
      new URL("../../internal/consoleui/static", import.meta.url),
    ),
    emptyOutDir: false,
    sourcemap: false,
    target: "es2022",
  },
});
