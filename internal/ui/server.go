/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package ui exposes a small HTTP UI for browsing Polls and submitting
// PollResponses. The Server implements sigs.k8s.io/controller-runtime's
// manager.Runnable interface so it can be added to a controller manager
// alongside the reconcilers and webhook server.
package ui

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Options configures the UI server.
type Options struct {
	// BindAddress is the "host:port" the HTTP server listens on. If empty,
	// the server is disabled.
	BindAddress string
}

// Server serves the Poll UI. It reads and writes Poll/PollResponse resources
// through the provided client, which should be the manager's cached client.
type Server struct {
	client  client.Client
	handler http.Handler
	addr    string
}

// New constructs a Server. Returns nil, nil when opts.BindAddress is empty so
// callers can conditionally register it without additional branches.
func New(c client.Client, opts Options) (*Server, error) {
	if opts.BindAddress == "" {
		return nil, nil
	}
	s := &Server{client: c, addr: opts.BindAddress}
	s.handler = s.routes()
	return s, nil
}

// NeedLeaderElection satisfies manager.LeaderElectionRunnable. The UI is safe
// to run on every manager replica: reads hit the cache and writes go through
// the API server (which admission-checks them), so there is no split-brain
// concern. Running it everywhere also means the service load-balancer can hit
// any pod.
func (s *Server) NeedLeaderElection() bool { return false }

// Start runs the HTTP server until ctx is cancelled. It satisfies
// manager.Runnable.
func (s *Server) Start(ctx context.Context) error {
	log := logf.FromContext(ctx).WithName("ui")

	srv := &http.Server{
		Addr:              s.addr,
		Handler:           s.handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("Starting UI server", "address", s.addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("UI server exited: %w", err)
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error(err, "UI server did not shut down cleanly")
		}
		// Drain the goroutine so we don't leak.
		<-errCh
		return nil
	case err := <-errCh:
		return err
	}
}
