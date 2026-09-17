package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
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
	server := &http.Server{
		Addr: fmt.Sprintf(":%d", cfg.Port), Handler: handler,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10,
		// Application middleware records safe failures; suppress net/http's raw
		// panic/error logger, which can otherwise include request or panic data.
		ErrorLog: log.New(io.Discard, "", 0),
	}
	watchCtx, cancelWatch := context.WithCancel(ctx)
	defer cancelWatch()
	watchDone := make(chan struct{})
	go func() { watcher.Run(watchCtx); close(watchDone) }()
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.ListenAndServe() }()
	logger.Info("portal lifecycle", "event", "server", "result", "started")
	success := true
	select {
	case <-ctx.Done():
	case err := <-serveDone:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			success = fail("server")
		}
	}
	cancelWatch()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		success = fail("shutdown")
	}
	select {
	case <-watchDone:
	case <-shutdownCtx.Done():
		success = fail("watch_shutdown")
	}
	logger.Info("portal lifecycle", "event", "shutdown", "result", "stopped")
	return success
}
