package dashboard

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	qweb "github.com/qorechain/qorechain-lightnode/web"
)

// Server serves the dashboard UI and REST API endpoints.
type Server struct {
	bindAddr string
	logger   *slog.Logger
	mux      *http.ServeMux
	srv      *http.Server
	api      *API
	hub      *Hub
}

// New creates a dashboard server bound to the given address.
func New(bindAddr string, api *API, logger *slog.Logger) *Server {
	s := &Server{
		bindAddr: bindAddr,
		logger:   logger,
		api:      api,
		hub:      NewHub(logger),
	}
	s.mux = http.NewServeMux()
	s.routes()
	return s
}

// Hub returns the WebSocket hub for external broadcast use.
func (s *Server) Hub() *Hub {
	return s.hub
}

func (s *Server) routes() {
	// REST API endpoints
	s.mux.HandleFunc("GET /api/status", s.api.HandleStatus)
	s.mux.HandleFunc("GET /api/validators", s.api.HandleValidators)
	s.mux.HandleFunc("GET /api/delegation", s.api.HandleDelegation)
	s.mux.HandleFunc("GET /api/rewards", s.api.HandleRewards)
	s.mux.HandleFunc("GET /api/network", s.api.HandleNetwork)
	s.mux.HandleFunc("GET /api/bridge", s.api.HandleBridge)
	s.mux.HandleFunc("GET /api/tokenomics", s.api.HandleTokenomics)
	s.mux.HandleFunc("GET /api/settings", s.api.HandleSettings)

	// WebSocket endpoint for real-time updates
	s.mux.HandleFunc("GET /ws", s.hub.HandleWS)

	// Static assets — serve the embedded web UI
	webFS, _ := fs.Sub(qweb.Assets, ".")
	fileServer := http.FileServer(http.FS(webFS))
	s.mux.Handle("/", fileServer)
}

// Start begins listening and serving. It blocks until the context is
// cancelled or the server encounters a fatal error.
func (s *Server) Start(ctx context.Context) error {
	s.srv = &http.Server{
		Addr:              s.bindAddr,
		Handler:           s.mux,
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.srv.Shutdown(shutCtx); err != nil {
			s.logger.Warn("dashboard server shutdown error", "error", err)
		}
	}()

	s.logger.Info("dashboard server starting", "addr", s.bindAddr)
	if err := s.srv.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}
