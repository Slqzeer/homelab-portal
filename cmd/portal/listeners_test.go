package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	portalhttp "github.com/Slqzeer/homelab-portal/internal/http"
)

func freeAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func TestListenerStartupFailureReleasesOtherPortAndLogsSafely(t *testing.T) {
	for _, failed := range []int{0, 1} {
		t.Run([]string{"public", "operations"}[failed], func(t *testing.T) {
			occupied, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer occupied.Close()
			servers := []*http.Server{{Addr: freeAddress(t)}, {Addr: freeAddress(t)}}
			servers[failed].Addr = occupied.Addr().String()
			var logs bytes.Buffer
			started := false
			if serve(context.Background(), portalhttp.NewLogger(&logs), servers[0], servers[1], func(context.Context) { started = true }, time.Second) {
				t.Fatal("occupied listener accepted")
			}
			if started {
				t.Fatal("watcher started before both listeners were bound")
			}
			other, err := net.Listen("tcp", servers[1-failed].Addr)
			if err != nil {
				t.Fatal("startup failure left other listener bound")
			}
			other.Close()
			if strings.Contains(logs.String(), occupied.Addr().String()) || !strings.Contains(logs.String(), `"result":"failed"`) {
				t.Fatal("listen error was disclosed or failure was not logged")
			}
		})
	}
}

func TestBothListenersDrainRequestsAndCancelWatcher(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var once sync.Once
	releaseRequests := func() { once.Do(func() { close(release) }) }
	defer releaseRequests()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { entered <- struct{}{}; <-release; w.WriteHeader(204) })
	public := &http.Server{Addr: freeAddress(t), Handler: handler}
	operations := &http.Server{Addr: freeAddress(t), Handler: handler}
	watchStarted, watchStopped := make(chan struct{}), make(chan struct{})
	done := make(chan bool, 1)
	go func() {
		done <- serve(ctx, portalhttp.NewLogger(io.Discard), public, operations, func(ctx context.Context) {
			close(watchStarted)
			<-ctx.Done()
			close(watchStopped)
		}, time.Second)
	}()
	select {
	case <-watchStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("watcher never started")
	}
	responses := make(chan int, 2)
	for _, server := range []*http.Server{public, operations} {
		go func(address string) {
			client := &http.Client{Timeout: 3 * time.Second}
			response, err := client.Get("http://" + address)
			if err != nil {
				responses <- 0
				return
			}
			response.Body.Close()
			responses <- response.StatusCode
		}(server.Addr)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("listener did not serve request")
		}
	}
	cancel()
	select {
	case <-watchStopped:
	case <-time.After(3 * time.Second):
		t.Fatal("watcher was not cancelled")
	}
	select {
	case <-done:
		t.Fatal("shutdown did not drain active requests")
	default:
	}
	releaseRequests()
	for i := 0; i < 2; i++ {
		if status := <-responses; status != 204 {
			t.Errorf("drained response=%d", status)
		}
	}
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("graceful shutdown failed")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown leaked a worker")
	}
	for _, server := range []*http.Server{public, operations} {
		l, err := net.Listen("tcp", server.Addr)
		if err != nil {
			t.Fatal("listener remained bound after shutdown")
		}
		l.Close()
	}
}

func TestBothListenersShareShutdownDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, stopped := make(chan struct{}, 2), make(chan struct{}, 2)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-r.Context().Done()
		stopped <- struct{}{}
	})
	public := &http.Server{Addr: freeAddress(t), Handler: handler}
	operations := &http.Server{Addr: freeAddress(t), Handler: handler}
	started := make(chan struct{})
	done := make(chan bool, 1)
	go func() {
		done <- serve(ctx, portalhttp.NewLogger(io.Discard), public, operations, func(ctx context.Context) { close(started); <-ctx.Done() }, 100*time.Millisecond)
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("startup timed out")
	}
	for _, server := range []*http.Server{public, operations} {
		go func(address string) {
			response, err := (&http.Client{Timeout: 3 * time.Second}).Get("http://" + address)
			if err == nil {
				response.Body.Close()
			}
		}(server.Addr)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("request never arrived")
		}
	}
	cancel()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("forced shutdown reported success")
		}
	case <-time.After(time.Second):
		t.Fatal("shared shutdown deadline exceeded")
	}
	for i := 0; i < 2; i++ {
		select {
		case <-stopped:
		case <-time.After(time.Second):
			t.Fatal("active request was not closed")
		}
	}
}
