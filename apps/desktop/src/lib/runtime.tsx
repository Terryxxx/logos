// Runtime config = { url, token } the Go server wrote to runtime.json.
// Resolved once at app boot via the Tauri get_runtime_config command,
// or — when running in pure web dev (`pnpm dev` without `pnpm tauri dev`)
// — falls back to localhost:7878 + a token from VITE_LOGOS_TOKEN.

import { createContext, useContext, useEffect, useState, type ReactNode } from "react";

type RuntimeConfig = { url: string; token: string; port: number };

const Ctx = createContext<RuntimeConfig | null>(null);

async function resolve(): Promise<RuntimeConfig> {
  // Tauri path
  if (typeof window !== "undefined" && (window as any).__TAURI_INTERNALS__) {
    const { invoke } = await import("@tauri-apps/api/core");
    return await invoke<RuntimeConfig>("get_runtime_config");
  }
  // Browser dev path
  const token = (import.meta.env.VITE_LOGOS_TOKEN as string | undefined) ?? "";
  if (!token) {
    throw new Error(
      "Browser dev mode: set VITE_LOGOS_TOKEN env var to the value in <data-dir>/runtime.json",
    );
  }
  return { url: "http://127.0.0.1:7878", token, port: 7878 };
}

export function RuntimeProvider({ children }: { children: ReactNode }) {
  const [cfg, setCfg] = useState<RuntimeConfig | null>(null);
  const [err, setErr] = useState<string | null>(null);
  useEffect(() => {
    resolve().then(setCfg).catch((e) => setErr(String(e)));
  }, []);
  if (err) {
    return (
      <div className="m-8 rounded border border-danger/40 bg-danger/10 p-6 text-sm">
        <div className="mb-2 font-semibold">Logos server not reachable</div>
        <div className="font-mono text-xs opacity-80">{err}</div>
        <div className="mt-4 opacity-80">
          Start it in another terminal:
          <pre className="mt-2 rounded bg-bg/60 p-3 font-mono text-xs">
            cd server && go run ./cmd/logos-server
          </pre>
        </div>
      </div>
    );
  }
  if (!cfg) {
    return <div className="grid h-full place-items-center text-sm opacity-60">Connecting…</div>;
  }
  return <Ctx.Provider value={cfg}>{children}</Ctx.Provider>;
}

export function useRuntimeConfig(): RuntimeConfig {
  const v = useContext(Ctx);
  if (!v) throw new Error("useRuntimeConfig outside RuntimeProvider");
  return v;
}
