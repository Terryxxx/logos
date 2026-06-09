#!/usr/bin/env node
/**
 * tauri-dev.mjs
 *
 * Wraps `tauri dev` to swallow Windows' exit-code 4294967295
 * (= -1 uint32 = STATUS_CONTROL_C_EXIT) that Tauri emits when the user
 * closes the dev window. pnpm otherwise surfaces it as
 *   ELIFECYCLE  Command failed with exit code 4294967295
 * which looks alarming but is harmless.
 *
 * This wrapper always exits 0. Compile errors, panics, etc. are visible
 * via stderr in the meantime, so the loss of signal here is acceptable
 * for a dev convenience script.
 */

import { spawn } from "node:child_process";

const child = spawn("tauri", ["dev"], {
  stdio: "inherit",
  shell: true, // Windows: resolve `tauri` via cmd/PATH
});

let exited = false;
const finish = (label, info) => {
  if (exited) return;
  exited = true;
  if (info) console.error(`[tauri-dev wrapper] tauri exited (${label} ${info})`);
  process.exit(0);
};

child.on("close", (code, signal) => finish("close", `code=${code} signal=${signal}`));
child.on("exit", (code, signal) => finish("exit", `code=${code} signal=${signal}`));
child.on("error", (err) => finish("error", err.message));

// Forward Ctrl+C to the child so cargo/tauri can clean up its own
// subprocesses before we exit.
process.on("SIGINT", () => {
  try {
    child.kill("SIGINT");
  } catch {
    /* ignore */
  }
});
