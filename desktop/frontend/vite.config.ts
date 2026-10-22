import { defineConfig } from "vite";
import preact from "@preact/preset-vite";
import tailwindcss from "@tailwindcss/postcss";

// The build goes to dist/, which the Wails app embeds. In the browser (vite dev or
// `vite preview`) the API calls are proxied to a running gateway, so the page is same-origin.
const gateway = process.env.KITTIES_GATEWAY || "http://127.0.0.1:7488";

export default defineConfig({
  plugins: [preact()],
  css: { postcss: { plugins: [tailwindcss()] } },
  server: { proxy: { "/v1": gateway } },
  preview: { proxy: { "/v1": gateway } },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    minify: "esbuild",
    target: "es2020",
    sourcemap: false,
  },
  resolve: { alias: { react: "preact/compat", "react-dom": "preact/compat" } },
});
