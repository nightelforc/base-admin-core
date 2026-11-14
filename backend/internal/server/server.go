package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/base-admin/base-admin-core/backend/internal/config"
	"github.com/base-admin/base-admin-core/backend/internal/middleware"
)

// Server owns the http.Server and the background writers that must be drained
// before the process exits.
type Server struct {
	http            *http.Server
	log             *slog.Logger
	operations      *middleware.OperationLogger
	shutdownTimeout time.Duration
}

// New assembles the server. Timeouts come from configuration so a deployment
// can tune them without a rebuild.
func New(cfg *config.Config, engine *gin.Engine, log *slog.Logger, operations *middleware.OperationLogger) *Server {
	return &Server{
		http: &http.Server{
			Addr:              cfg.HTTP.Addr,
			Handler:           engine,
			ReadHeaderTimeout: cfg.HTTP.ReadHeaderTimeout,
			ReadTimeout:       cfg.HTTP.ReadTimeout,
			WriteTimeout:      cfg.HTTP.WriteTimeout,
			IdleTimeout:       cfg.HTTP.IdleTimeout,
		},
		log:             log,
		operations:      operations,
		shutdownTimeout: cfg.HTTP.ShutdownTimeout,
	}
}

// Run serves until ctx is cancelled, then shuts down gracefully: in-flight
// requests get the configured grace period and queued audit entries are flushed.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		s.log.Info("http server listening", "addr", s.http.Addr)
		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		s.drain()
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
	defer cancel()

	s.log.Info("shutting down http server")
	if err := s.http.Shutdown(shutdownCtx); err != nil {
		s.log.Error("graceful shutdown failed", "error", err)
	}
	s.drain()
	return nil
}

func (s *Server) drain() {
	if s.operations != nil {
		s.operations.Close()
	}
}
