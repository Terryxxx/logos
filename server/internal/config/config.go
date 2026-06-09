// Package config resolves runtime locations (data dir, ports, runtime.json)
// for the Logos local server.
package config

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
)

const (
	defaultPort   = 7878
	runtimeFile   = "runtime.json"
	dbFileName    = "logos.db"
	appFolderName = "Logos"
)

type Config struct {
	DataDir       string
	PreferredPort int
}

func Load() (*Config, error) {
	dir, err := resolveDataDir()
	if err != nil {
		return nil, err
	}
	if env := os.Getenv("LOGOS_DATA_DIR"); env != "" {
		dir = env
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("ensure data dir: %w", err)
	}

	port := defaultPort
	if env := os.Getenv("LOGOS_PORT"); env != "" {
		if v, err := strconv.Atoi(env); err == nil && v > 0 && v < 65536 {
			port = v
		}
	}

	return &Config{DataDir: dir, PreferredPort: port}, nil
}

func (c *Config) DBPath() string          { return filepath.Join(c.DataDir, dbFileName) }
func (c *Config) RuntimeFilePath() string { return filepath.Join(c.DataDir, runtimeFile) }

// WriteRuntimeFile writes the bound address + auth token so the Tauri main
// process can hand them to the webview. Rewritten on every server start.
func (c *Config) WriteRuntimeFile(addr, token string) error {
	rec := map[string]any{
		"addr":  addr,
		"port":  portOf(addr),
		"token": token,
		"pid":   os.Getpid(),
	}
	b, _ := json.MarshalIndent(rec, "", "  ")
	return os.WriteFile(c.RuntimeFilePath(), b, 0o600)
}

// BindLocal listens on 127.0.0.1:port; on EADDRINUSE walks up by 1 until free.
// Returns the resolved addr ("127.0.0.1:7878") and the open listener.
func BindLocal(preferred int) (string, net.Listener, error) {
	for p := preferred; p < preferred+100; p++ {
		addr := fmt.Sprintf("127.0.0.1:%d", p)
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			return addr, ln, nil
		}
	}
	return "", nil, fmt.Errorf("no free port in [%d, %d)", preferred, preferred+100)
}

func portOf(addr string) int {
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	v, _ := strconv.Atoi(p)
	return v
}

// resolveDataDir picks the OS-conventional per-user app data directory.
func resolveDataDir() (string, error) {
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("APPDATA")
		if base == "" {
			return "", fmt.Errorf("APPDATA not set")
		}
		return filepath.Join(base, appFolderName), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", appFolderName), nil
	default: // linux / *bsd
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, appFolderName), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", appFolderName), nil
	}
}
