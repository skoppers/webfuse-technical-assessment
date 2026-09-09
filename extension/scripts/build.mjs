// Builds the extension into dist/ as flat files matching the Webfuse extension layout.
// One IIFE bundle per component; static files are copied verbatim.
import * as esbuild from "esbuild";
import { cpSync, mkdirSync, rmSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const src = join(root, "src");
const dist = join(root, "dist");
const watch = process.argv.includes("--watch");

rmSync(dist, { recursive: true, force: true });
mkdirSync(dist, { recursive: true });

for (const file of ["manifest.json", "popup.html", "icon24.png"]) {
  cpSync(join(src, file), join(dist, file));
}

const ctx = await esbuild.context({
  entryPoints: {
    background: join(src, "background/index.ts"),
    content: join(src, "content.ts"),
    popup: join(src, "popup.ts"),
  },
  outdir: dist,
  bundle: true,
  format: "iife",
  target: "es2020",
  platform: "browser",
  sourcemap: false,
  logLevel: "info",
});

if (watch) {
  await ctx.watch();
  console.log("watching src/ …");
} else {
  await ctx.rebuild();
  await ctx.dispose();
}
