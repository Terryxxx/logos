//! Logos Tauri shell.
//!
//! Responsibilities:
//!   1. Spawn `logos-server` (Go) as a sidecar process. Lifetime is bound
//!      to the app; killed on exit.
//!   2. Wait for the server to write `<data-dir>/runtime.json`, then
//!      hand the URL+token to the React UI via the `get_runtime_config`
//!      command.
//!   3. Forward server stdout/stderr to the Tauri console for debugging.
//!
//! Bypass mode: setting `LOGOS_SIDECAR=off` in the environment skips the
//! spawn and lets the user run `go run ./cmd/logos-server` in another
//! terminal -- handy when hacking on the Go side with `go run`'s hot
//! re-compile, since the bundled sidecar is incrementally rebuilt by
//! `scripts/bundle-sidecar.mjs` but never live-reloaded.

use std::{
    fs,
    path::{Path, PathBuf},
    sync::Mutex,
    thread,
    time::{Duration, Instant},
};

use serde::Serialize;
use tauri::Manager;
use tauri_plugin_opener::OpenerExt;
use tauri_plugin_shell::{
    process::{CommandChild, CommandEvent},
    ShellExt,
};

#[derive(Debug, Serialize)]
pub struct RuntimeConfig {
    url: String,
    token: String,
    port: u16,
}

#[derive(Debug, Serialize)]
pub struct CommandError {
    message: String,
}

impl From<String> for CommandError {
    fn from(message: String) -> Self {
        Self { message }
    }
}

/// SidecarState owns the spawned Go server's process handle so the
/// app's RunEvent::ExitRequested hook can kill it.
struct SidecarState {
    child: Mutex<Option<CommandChild>>,
}

fn data_dir() -> Result<PathBuf, String> {
    if cfg!(target_os = "windows") {
        let base = std::env::var("APPDATA").map_err(|_| "APPDATA not set".to_string())?;
        Ok(PathBuf::from(base).join("Logos"))
    } else if cfg!(target_os = "macos") {
        let home = std::env::var("HOME").map_err(|_| "HOME not set".to_string())?;
        Ok(PathBuf::from(home)
            .join("Library")
            .join("Application Support")
            .join("Logos"))
    } else {
        let home = std::env::var("HOME").map_err(|_| "HOME not set".to_string())?;
        let base = std::env::var("XDG_DATA_HOME")
            .map(PathBuf::from)
            .unwrap_or_else(|_| PathBuf::from(home).join(".local").join("share"));
        Ok(base.join("Logos"))
    }
}

fn read_runtime(path: &Path) -> Result<RuntimeConfig, String> {
    let raw = fs::read_to_string(path).map_err(|e| e.to_string())?;
    let v: serde_json::Value = serde_json::from_str(&raw).map_err(|e| e.to_string())?;
    let addr = v
        .get("addr")
        .and_then(|x| x.as_str())
        .ok_or_else(|| "runtime.json missing 'addr'".to_string())?
        .to_string();
    let token = v
        .get("token")
        .and_then(|x| x.as_str())
        .ok_or_else(|| "runtime.json missing 'token'".to_string())?
        .to_string();
    let port = v.get("port").and_then(|x| x.as_u64()).unwrap_or(7878) as u16;
    Ok(RuntimeConfig {
        url: format!("http://{}", addr),
        token,
        port,
    })
}

/// Polls runtime.json for up to ~10s. The sidecar typically writes it
/// within 100-500 ms on warm starts, but a cold modernc-sqlite init on
/// first launch can take a couple seconds.
#[tauri::command]
fn get_runtime_config() -> Result<RuntimeConfig, CommandError> {
    let path = data_dir()?.join("runtime.json");
    let deadline = Instant::now() + Duration::from_secs(10);
    let mut last_err = String::new();
    while Instant::now() < deadline {
        match read_runtime(&path) {
            Ok(cfg) => return Ok(cfg),
            Err(e) => last_err = e,
        }
        thread::sleep(Duration::from_millis(50));
    }
    Err(format!(
        "timed out waiting for {} (last error: {}). \
         If you set LOGOS_SIDECAR=off, start the server in another terminal: \
         cd server && go run ./cmd/logos-server",
        path.display(),
        last_err
    )
    .into())
}

fn sidecar_disabled() -> bool {
    std::env::var("LOGOS_SIDECAR")
        .map(|v| matches!(v.to_ascii_lowercase().as_str(), "off" | "0" | "false" | "no"))
        .unwrap_or(false)
}

/// Open an absolute path in the OS file explorer / Finder / file manager.
/// Used by the "Open workspace" button on task cards so the user can
/// inspect / open / edit the files the agent produced.
///
/// Refuses any non-absolute path, and refuses paths whose canonical form
/// escapes the per-user data dir (the sandbox we created for ourselves).
/// This is a UI-convenience command, not a general FS-browse capability.
#[tauri::command]
fn open_path(app: tauri::AppHandle, path: String) -> Result<(), CommandError> {
    let p = std::path::Path::new(&path);
    if !p.is_absolute() {
        return Err(format!("refusing relative path: {path}").into());
    }
    let canonical = p
        .canonicalize()
        .map_err(|e| format!("canonicalize {path}: {e}"))?;
    let dir = data_dir()?;
    let dir_canonical = dir
        .canonicalize()
        .map_err(|e| format!("canonicalize data dir: {e}"))?;
    if !canonical.starts_with(&dir_canonical) {
        return Err(format!(
            "refusing path outside data dir ({} not under {})",
            canonical.display(),
            dir_canonical.display()
        )
        .into());
    }
    app.opener()
        .open_path(canonical.to_string_lossy().to_string(), None::<&str>)
        .map_err(|e| format!("open path: {e}").into())
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    let app = tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_opener::init())
        .manage(SidecarState {
            child: Mutex::new(None),
        })
        .setup(|app| {
            if sidecar_disabled() {
                eprintln!(
                    "[logos] LOGOS_SIDECAR=off; skipping sidecar. \
                     Run `go run ./cmd/logos-server` in another terminal."
                );
                return Ok(());
            }

            // Wipe any stale runtime.json so we never hand the UI a port
            // pointing at a server that died on the previous run.
            if let Ok(dir) = data_dir() {
                let rt = dir.join("runtime.json");
                if rt.exists() {
                    let _ = fs::remove_file(&rt);
                }
            }

            let sidecar = app
                .shell()
                .sidecar("logos-server")
                .map_err(|e| format!("locate sidecar: {e}"))?;

            let (mut rx, child) = sidecar
                .spawn()
                .map_err(|e| format!("spawn sidecar: {e}"))?;

            eprintln!("[logos] sidecar spawned (pid={})", child.pid());
            *app.state::<SidecarState>().child.lock().unwrap() = Some(child);

            // Forward server output. The server already prefixes its lines
            // with slog timestamps, so we don't add anything except the
            // [server] tag to distinguish from Tauri's own messages.
            tauri::async_runtime::spawn(async move {
                while let Some(event) = rx.recv().await {
                    match event {
                        CommandEvent::Stdout(line) | CommandEvent::Stderr(line) => {
                            eprintln!("[server] {}", String::from_utf8_lossy(&line).trim_end());
                        }
                        CommandEvent::Error(e) => {
                            eprintln!("[server] error: {e}");
                        }
                        CommandEvent::Terminated(payload) => {
                            eprintln!(
                                "[server] exited (code={:?}, signal={:?})",
                                payload.code, payload.signal
                            );
                        }
                        _ => {}
                    }
                }
            });

            Ok(())
        })
        .invoke_handler(tauri::generate_handler![get_runtime_config, open_path])
        .build(tauri::generate_context!())
        .expect("error while building tauri application");

    app.run(|handle, event| {
        if let tauri::RunEvent::ExitRequested { .. } = event {
            if let Some(state) = handle.try_state::<SidecarState>() {
                if let Some(child) = state.child.lock().unwrap().take() {
                    eprintln!("[logos] killing sidecar (pid={})", child.pid());
                    let _ = child.kill();
                }
            }
        }
    });
}
