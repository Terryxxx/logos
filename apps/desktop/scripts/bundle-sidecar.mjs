#!/usr/bin/env node
/**
 * bundle-sidecar.mjs
 *
 * Builds the Go server binary and drops it at the exact path Tauri's
 * sidecar machinery expects:
 *
 *   apps/desktop/src-tauri/binaries/logos-server-<HOST_TRIPLE>[.exe]
 *
 * Run automatically before `tauri dev` and `tauri build` via the
 * `pnpm tauri:dev` / `pnpm tauri:build` npm scripts.
 *
 * Skips the build when the existing binary is newer than every Go source
 * file -- so the dev loop stays fast (a second `pnpm tauri:dev` with no
 * Go changes finishes in <100 ms).
 */

import { execSync, spawnSync } from "node:child_process";
import {
  existsSync,
  mkdirSync,
  readdirSync,
  statSync,
} from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
// scripts/ -> desktop/ -> apps/ -> repo root
const REPO_ROOT = resolve(__dirname, "..", "..", "..");
const SERVER_DIR = join(REPO_ROOT, "server");
const BIN_DIR = resolve(__dirname, "..", "src-tauri", "binaries");

const log = (...args) => console.log("[bundle-sidecar]", ...args);

function detectHostTriple() {
  const out = execSync("rustc -vV", { encoding: "utf8" });
  const m = out.match(/host:\s+(\S+)/);
  if (!m) throw new Error("could not parse host triple from `rustc -vV`");
  return m[1];
}

function walkGoFiles(dir) {
  const out = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === "tmp" || entry.name === "bin") continue;
    const p = join(dir, entry.name);
    if (entry.isDirectory()) out.push(...walkGoFiles(p));
    else if (entry.name.endsWith(".go") || entry.name === "go.mod" || entry.name === "go.sum") {
      out.push(p);
    } else if (entry.name.endsWith(".sql") && dir.endsWith("migrations")) {
      out.push(p);
    }
  }
  return out;
}

function newestMtime(files) {
  let max = 0;
  for (const f of files) {
    const m = statSync(f).mtimeMs;
    if (m > max) max = m;
  }
  return max;
}

function main() {
  const triple = detectHostTriple();
  const ext = triple.includes("windows") ? ".exe" : "";
  const out = join(BIN_DIR, `logos-server-${triple}${ext}`);

  mkdirSync(BIN_DIR, { recursive: true });

  // Incremental: rebuild only when a Go source file is newer than the binary.
  // This keeps the dev loop snappy (re-runs are sub-second when nothing changed).
  const sources = walkGoFiles(SERVER_DIR);
  const newestSrc = newestMtime(sources);
  if (existsSync(out)) {
    const binMtime = statSync(out).mtimeMs;
    if (binMtime >= newestSrc) {
      log(`up to date: ${out}`);
      return;
    }
    log(`rebuilding (binary older than sources by ${Math.round((newestSrc - binMtime) / 1000)}s)`);
  } else {
    log(`first build for triple ${triple}`);
  }

  log(`go build -> ${out}`);
  const r = spawnSync(
    "go",
    ["build", "-o", out, "./cmd/logos-server"],
    {
      cwd: SERVER_DIR,
      stdio: "inherit",
      // Windows needs go.exe resolved via the shell; on Unix we keep
      // shell=false to avoid the DEP0190 warning about unescaped args.
      shell: process.platform === "win32" ? true : false,
    },
  );
  if (r.status !== 0) {
    console.error("[bundle-sidecar] go build failed");
    process.exit(r.status ?? 1);
  }
  log("done");
}

main();
