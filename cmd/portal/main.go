package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Slqzeer/homelab-portal/internal/assets"
	"github.com/Slqzeer/homelab-portal/internal/auth"
	"github.com/Slqzeer/homelab-portal/internal/catalog"
	"github.com/Slqzeer/homelab-portal/internal/config"
	portalhttp "github.com/Slqzeer/homelab-portal/internal/http"
	"github.com/Slqzeer/homelab-portal/internal/kube"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/types"
	networkingclient "k8s.io/client-go/kubernetes/typed/networking/v1"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

func main() {
	if !run(portalhttp.NewLogger(os.Stdout)) {
		os.Exit(1)
	}
}

func run(logger *slog.Logger) bool {
	// Configure process-global dependency logging before clients or background
	// workers start. client-go can log raw errors and URLs outside request
	// contexts; the watcher supplies our allowlisted operational events instead.
	klog.SetLoggerWithOptions(logr.Discard(), klog.ContextualLogger(true))
	klog.EnableContextualLogging(false)
	fail := func(event string) bool {
		logger.Error("portal lifecycle", "event", event, "result", "failed")
		return false
	}
	cfg, err := config.Load(os.Environ())
	if err != nil {
		return fail("configuration")
	}
	sessions, err := auth.NewSessionManager(cfg.SessionCurrentKey, cfg.SessionPreviousKey)
	if err != nil {
		return fail("session_setup")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	discoveryCtx, cancelDiscovery := context.WithTimeout(ctx, 10*time.Second)
	// go-oidc retains this client for background JWKS refreshes, which do
	// not inherit the callback request deadline.
	discoveryCtx = oidc.ClientContext(discoveryCtx, &http.Client{Timeout: 10 * time.Second})
	provider, err := auth.NewOIDC(discoveryCtx, cfg)
	cancelDiscovery()
	if err != nil {
		return fail("oidc_setup")
	}
	kubeConfig, err := rest.InClusterConfig()
	if err != nil {
		return fail("kubernetes_setup")
	}
	kubeConfig.WarningHandler = rest.NoWarnings{}
	client, err := networkingclient.NewForConfig(kubeConfig)
	if err != nil {
		return fail("kubernetes_setup")
	}
	store := catalog.NewStore(types.NamespacedName{Namespace: cfg.PortalIngressNamespace, Name: cfg.PortalIngressName})
	watcher := kube.NewWatcher(kube.NewIngressSource(client.Ingresses("")), store, kube.WithLogger(logger))
	handler, err := portalhttp.New(portalhttp.Options{Store: store, Sessions: sessions, OIDC: provider, Limiter: auth.NewLoginLimiter(), Assets: assets.FS(), BaseURL: cfg.PortalBaseURL, Logger: logger, Watcher: watcher, Registry: prometheus.NewRegistry()})
	if err != nil {
		return fail("http_setup")
	}
	return serve(ctx, logger, newServer(cfg.Port, handler.Public), newServer(cfg.OperationsPort, handler.Operations), watcher.Run, 20*time.Second)
}

func newServer(port int, handler http.Handler) *http.Server {
	return &http.Server{
		Addr: fmt.Sprintf(":%d", port), Handler: handler,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10,
		// Application middleware records safe failures; suppress net/http's raw
		// panic/error logger, which can otherwise include request or panic data.
		ErrorLog: log.New(io.Discard, "", 0),
	}
}

// Bind both ports before starting any background work. A failed bind cannot
// leave a partial application running or log addresses from raw network errors.
func serve(ctx context.Context, logger *slog.Logger, public, operations *http.Server, watch func(context.Context), grace time.Duration) bool {
	fail := func(event string) bool {
		logger.Error("portal lifecycle", "event", event, "result", "failed")
		return false
	}
	servers := []*http.Server{public, operations}
	listeners := make([]net.Listener, 0, len(servers))
	for i, server := range servers {
		listener, err := net.Listen("tcp", server.Addr)
		if err != nil {
			for _, bound := range listeners {
				_ = bound.Close()
			}
			return fail([]string{"public_listen", "operations_listen"}[i])
		}
		listeners = append(listeners, listener)
	}
	watchCtx, cancelWatch := context.WithCancel(ctx)
	defer cancelWatch()
	watchDone := make(chan struct{})
	go func() { watch(watchCtx); close(watchDone) }()
	serveDone := make(chan error, len(servers))
	for i, server := range servers {
		go func() { serveDone <- server.Serve(listeners[i]) }()
	}
	logger.Info("portal lifecycle", "event", "server", "result", "started")
	success := true
	remaining := len(servers)
	select {
	case <-ctx.Done():
	case err := <-serveDone:
		remaining--
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			success = fail("server")
		}
	}
	cancelWatch()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), grace)
	defer cancelShutdown()
	shutdownDone := make(chan error, len(servers))
	for _, server := range servers {
		go func() {
			err := server.Shutdown(shutdownCtx)
			if err != nil {
				_ = server.Close()
			}
			shutdownDone <- err
		}()
	}
	for range servers {
		if err := <-shutdownDone; err != nil {
			success = fail("shutdown")
		}
	}
	for ; remaining > 0; remaining-- {
		if err := <-serveDone; err != nil && !errors.Is(err, http.ErrServerClosed) {
			success = fail("server")
		}
	}
	select {
	case <-watchDone:
	case <-shutdownCtx.Done():
		success = fail("watch_shutdown")
	}
	logger.Info("portal lifecycle", "event", "shutdown", "result", "stopped")
	return success
}
