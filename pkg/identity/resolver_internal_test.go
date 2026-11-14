// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package identity

import (
	"context"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestResolver starts a server that counts every request and returns a
// resolver whose retry delays are zero so the retry tests stay fast.
func newTestResolver(
	t *testing.T,
	handler http.HandlerFunc,
) (*sentryIdentityResolver, *atomic.Int32) {
	t.Helper()
	attempts := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return &sentryIdentityResolver{
		client:      server.Client(),
		baseURL:     server.URL,
		org:         "bborbe",
		token:       "test-token",
		retryDelays: []time.Duration{0, 0},
	}, attempts
}

func TestResolveDoesNotRetryNotFound(t *testing.T) {
	resolver, attempts := newTestResolver(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"detail":"no such issue"}`))
	})

	got, err := resolver.Resolve(context.Background(), "NUKE-DEV-FB")
	if err == nil {
		t.Fatal("expected a 404 to fail")
	}
	if got != "" {
		t.Fatalf("expected empty identity, got %q", got)
	}
	if count := attempts.Load(); count != 1 {
		t.Fatalf("expected 1 attempt, got %d", count)
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected error to name status 404, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "no such issue") {
		t.Fatalf("expected error to carry the body, got %q", err.Error())
	}
}

func TestResolveDoesNotRetryBadRequest(t *testing.T) {
	resolver, attempts := newTestResolver(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})

	if _, err := resolver.Resolve(context.Background(), "NUKE-DEV-FB"); err == nil {
		t.Fatal("expected a 400 to fail")
	}
	if count := attempts.Load(); count != 1 {
		t.Fatalf("expected 1 attempt, got %d", count)
	}
}

func TestResolveRetriesRateLimitThenSucceeds(t *testing.T) {
	var attempts *atomic.Int32
	resolver, attempts := newTestResolver(t, func(w http.ResponseWriter, r *http.Request) {
		if attempts.Load() <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"shortId":"NUKE-DEV-FB"}`))
	})

	got, err := resolver.Resolve(context.Background(), "NUKE-DEV-FB")
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if got != "NUKE-DEV-FB" {
		t.Fatalf("expected NUKE-DEV-FB, got %q", got)
	}
	if count := attempts.Load(); count != 3 {
		t.Fatalf("expected 3 attempts, got %d", count)
	}
}

func TestResolveRetriesServerErrorThenSucceeds(t *testing.T) {
	var attempts *atomic.Int32
	resolver, attempts := newTestResolver(t, func(w http.ResponseWriter, r *http.Request) {
		if attempts.Load() <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"shortId":"NUKE-DEV-FB"}`))
	})

	got, err := resolver.Resolve(context.Background(), "NUKE-DEV-FB")
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if got != "NUKE-DEV-FB" {
		t.Fatalf("expected NUKE-DEV-FB, got %q", got)
	}
	if count := attempts.Load(); count != 3 {
		t.Fatalf("expected 3 attempts, got %d", count)
	}
}

func TestResolveExhaustsRetriesOnPersistentServerError(t *testing.T) {
	resolver, attempts := newTestResolver(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"detail":"boom"}`))
	})

	got, err := resolver.Resolve(context.Background(), "NUKE-DEV-FB")
	if err == nil {
		t.Fatal("expected a persistent 500 to fail")
	}
	if got != "" {
		t.Fatalf("expected empty identity, got %q", got)
	}
	if count := attempts.Load(); count != 3 {
		t.Fatalf("expected 3 attempts, got %d", count)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected error to name status 500, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected error to carry the body, got %q", err.Error())
	}
}

func TestNewSentryIdentityResolverAddsTimeoutWithoutMutation(t *testing.T) {
	caller := &http.Client{}
	resolver, ok := NewSentryIdentityResolver(caller, "", "", "token").(*sentryIdentityResolver)
	if !ok {
		t.Fatal("expected *sentryIdentityResolver")
	}
	if resolver.client.Timeout != defaultHTTPClientTimeout {
		t.Fatalf("expected %v, got %v", defaultHTTPClientTimeout, resolver.client.Timeout)
	}
	if caller.Timeout != 0 {
		t.Fatalf("expected caller client to stay unmutated, got %v", caller.Timeout)
	}
}

func TestNewSentryIdentityResolverPreservesExplicitTimeout(t *testing.T) {
	caller := &http.Client{Timeout: 3 * time.Second}
	resolver, ok := NewSentryIdentityResolver(caller, "", "", "token").(*sentryIdentityResolver)
	if !ok {
		t.Fatal("expected *sentryIdentityResolver")
	}
	if resolver.client.Timeout != 3*time.Second {
		t.Fatalf("expected 3s, got %v", resolver.client.Timeout)
	}
}

func TestNewSentryIdentityResolverDefaultsNilClient(t *testing.T) {
	resolver, ok := NewSentryIdentityResolver(nil, "", "", "token").(*sentryIdentityResolver)
	if !ok {
		t.Fatal("expected *sentryIdentityResolver")
	}
	if resolver.client == nil {
		t.Fatal("expected a non-nil client")
	}
	if resolver.client.Timeout != defaultHTTPClientTimeout {
		t.Fatalf("expected %v, got %v", defaultHTTPClientTimeout, resolver.client.Timeout)
	}
}

func TestWaitForRetryReturnsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := waitForRetry(ctx, time.Hour)
	if !stderrors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
