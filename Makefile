.PHONY: help dev server desktop install build clean

help: ## Show this help
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z0-9_.-]+:.*## / {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

install: ## Install Go deps + pnpm deps
	cd server && go mod tidy
	cd apps/desktop && pnpm install

server: ## Run Go server in foreground
	cd server && go run ./cmd/logos-server

desktop: ## Run Tauri dev shell (assumes server is already running)
	cd apps/desktop && pnpm tauri dev

dev: ## Run server + desktop together (requires 'make' parallel; or run each in its own terminal)
	@echo "Open two terminals and run 'make server' and 'make desktop' separately."
	@echo "(Concurrent run via Make is shell-dependent; will be replaced by Tauri sidecar in V0.2.)"

build: ## Build Tauri release bundle (includes Go server as sidecar)
	cd server && GOOS=$$(go env GOOS) GOARCH=$$(go env GOARCH) go build -o ../apps/desktop/src-tauri/binaries/logos-server-$$(rustc -vV | sed -n 's/host: //p') ./cmd/logos-server
	cd apps/desktop && pnpm tauri build

clean: ## Remove build artifacts
	rm -rf server/bin server/tmp
	rm -rf apps/desktop/src-tauri/target apps/desktop/src-tauri/binaries
	rm -rf apps/desktop/dist apps/desktop/node_modules

diagnose: ## Run end-to-end health check (PowerShell)
	pwsh -NoProfile -File scripts/diagnose.ps1
