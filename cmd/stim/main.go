// Command stim serves the Stim web UI and API.
package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ungerdas/stim/internal/api"
	"github.com/ungerdas/stim/internal/manager"
	"github.com/ungerdas/stim/internal/store"
	"github.com/ungerdas/stim/internal/transfer"
	"github.com/ungerdas/stim/internal/webui"

	// Client adapters register themselves on import. Add new ones here.
	_ "github.com/ungerdas/stim/internal/client/deluge"
	_ "github.com/ungerdas/stim/internal/client/qbittorrent"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// hostKeys stores an SSH host key seen on first contact and lets the
// manager pick up the changed instance.
type hostKeys struct {
	store   *store.Store
	manager *manager.Manager
}

func (h hostKeys) SetHostKey(id, key string) error {
	if err := h.store.SetHostKey(id, key); err != nil {
		return err
	}
	h.manager.Reload()
	return nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	// Stim has no login of its own yet, so it only listens on localhost
	// unless told otherwise. The Docker image sets STIM_HOST=0.0.0.0.
	host := env("STIM_HOST", "127.0.0.1")
	port := env("STIM_PORT", "7846")
	configDir := env("STIM_CONFIG_DIR", "config")

	st, err := store.Open(configDir)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			log.Fatalf("open config: %v (the config directory %q must be writable by the user Stim runs as)", err, configDir)
		}
		log.Fatalf("open config: %v", err)
	}
	mgr := manager.New(st)
	transfers := transfer.New(mgr, hostKeys{st, mgr}, transfer.DefaultTiming)

	srv := &http.Server{
		Addr:              net.JoinHostPort(host, port),
		Handler:           api.New(st, mgr, transfers, webui.FS(), version),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("stim %s listening on http://%s (config: %s)", version, srv.Addr, configDir)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
