package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Slqzeer/homelab-portal/internal/catalog"
	portalhttp "github.com/Slqzeer/homelab-portal/internal/http"
	"github.com/Slqzeer/homelab-portal/internal/kube"
	"k8s.io/apimachinery/pkg/types"
	networkingclient "k8s.io/client-go/kubernetes/typed/networking/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/flowcontrol"
	"k8s.io/klog/v2"
)

type failingAPITransport struct{}

func (failingAPITransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: failingAPIBody{}, Request: request}, nil
}

type failingAPIBody struct{}

func (failingAPIBody) Read([]byte) (int, error) {
	return 0, errors.New("private-response-body https://private-api.example/?private-query=secret Authorization: private-header")
}

func (failingAPIBody) Close() error { return nil }

func TestProductionLogsSuppressKubernetesDependencyDetails(t *testing.T) {
	// klog configuration is process-global and must precede client construction.
	// A subprocess exercises startup without changing other tests' logger state.
	if os.Getenv("PORTAL_TEST_DEPENDENCY_LOGGING") == "1" {
		if err := os.Setenv("OIDC_ISSUER_URL", ""); err != nil {
			t.Fatal(err)
		}
		logger := portalhttp.NewLogger(os.Stdout)
		if run(logger) {
			t.Fatal("startup accepted missing configuration")
		}
		client, err := networkingclient.NewForConfig(&rest.Config{
			Host:      "https://private-api.example/private-path",
			Transport: failingAPITransport{},
			// The second request exercises client-go's level-zero throttle log.
			RateLimiter: flowcontrol.NewTokenBucketRateLimiter(0.5, 1),
		})
		if err != nil {
			t.Fatal(err)
		}
		source := kube.NewIngressSource(client.Ingresses(""))
		if _, err := source.List(context.Background()); err == nil || !strings.Contains(err.Error(), "private-response-body") {
			t.Fatal("fixture did not reproduce the response-body failure")
		}
		watcher := kube.NewWatcher(source, catalog.NewStore(types.NamespacedName{}), kube.WithLogger(logger), kube.WithSleeper(func(context.Context, time.Duration) error { return context.Canceled }))
		watcher.Run(context.Background())
		// Transport background workers use both global/context-free klog paths.
		done := make(chan struct{})
		go func() {
			klog.Background().Error(errors.New("private-background-error"), "private transport detail", "URL", "https://private-api.example/?private-query=secret")
			klog.Warningf("private-background-warning Authorization: private-header")
			close(done)
		}()
		<-done
		klog.Flush()
		os.Exit(0) // Do not mix the test runner's PASS line into process logs.
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProductionLogsSuppressKubernetesDependencyDetails$")
	cmd.Env = append(os.Environ(), "PORTAL_TEST_DEPENDENCY_LOGGING=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("logging fixture failed: %v\nstdout: %s\nstderr: %s", err, &stdout, &stderr)
	}
	if stderr.Len() != 0 {
		t.Errorf("dependency output reached stderr: %s", &stderr)
	}
	if strings.Contains(stdout.String()+stderr.String(), "private-") {
		t.Error("dependency body, URL, query, or header escaped the logging boundary")
	}
	foundWatchFailure := false
	for _, line := range strings.Split(strings.TrimSpace(stdout.String()), "\n") {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Errorf("non-JSON production output: %q", line)
			continue
		}
		for key := range event {
			switch key {
			case "time", "level", "msg", "request_id", "event", "result":
			default:
				t.Errorf("unapproved production field: %s", key)
			}
		}
		if event["event"] == "list" && event["result"] == "failed" {
			foundWatchFailure = true
		}
	}
	if !foundWatchFailure {
		t.Error("allowlisted watcher failure was not retained")
	}
}
