//! Logos Tauri shell.
//!
//! Responsibilities:
//!   1. Locate the per-user data dir (OS-conventional) so we can find the
//!      `runtime.json` file the Go server writes on every startup.
//!   2. Expose a `get_runtime_config` command the React UI calls on mount
//!      to receive { url, token }.
//!
//! V0.1 deliberately does NOT auto-spawn the Go server — the developer
//! runs `go run ./cmd/logos-server` in a second terminal. Sidecar
//! integration (production bundling) lands in V0.2.

use serde::Serialize;
use std::{fs, path::PathBuf};

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

fn data_dir() -> Result<PathBuf, String> {
    // Cross-platform OS-conventional data dir, mirroring config.resolveDataDir in Go.
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

#[tauri::command]
fn get_runtime_config() -> Result<RuntimeConfig, CommandError> {
    let path = data_dir()?.join("runtime.json");
    let raw = fs::read_to_string(&path).map_err(|e| {
        format!(
            "could not read {} — start the Logos server first (run `go run ./cmd/logos-server` in /server). os: {}",
            path.display(),
            e
        )
    })?;
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

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .invoke_handler(tauri::generate_handler![get_runtime_config])
        .run(tauri::generate_context!())
        .expect("error while running tauri application");
}
