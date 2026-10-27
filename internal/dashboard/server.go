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
	token    string
	logger   *slog.Logger
	mux      *http.ServeMux
	srv      *http.Server
	api      *API
	hub      *Hub
}

// New creates a dashboard server bound to the given address. The token, when
// non-empty, is required on every data route; see auth.go.
func New(bindAddr string, api *API, logger *slog.Logger) *Server {
	token := ""
	if api != nil {
		token = api.cfg.Dashboard.Token
	}
	s := &Server{
		bindAddr: bindAddr,
		token:    token,
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
	// Data routes go behind the bearer token when one is configured. api is a
	// separate mux so one wrap covers every route, present and future: adding
	// a handler to an authenticated mux cannot forget the check.
	api := http.NewServeMux()

	// REST API endpoints
	api.HandleFunc("GET /api/status", s.api.HandleStatus)
	api.HandleFunc("GET /api/validators", s.api.HandleValidators)
	api.HandleFunc("GET /api/delegation", s.api.HandleDelegation)
	api.HandleFunc("GET /api/rewards", s.api.HandleRewards)
	api.HandleFunc("GET /api/rewardpool", s.api.HandleRewardPool)
	api.HandleFunc("GET /api/network", s.api.HandleNetwork)
	api.HandleFunc("GET /api/bridge", s.api.HandleBridge)
	api.HandleFunc("GET /api/tokenomics", s.api.HandleTokenomics)
	api.HandleFunc("GET /api/settings", s.api.HandleSettings)
	api.HandleFunc("GET /api/update", s.api.HandleUpdate)
	// Alias so the embedded web UI's fetch("/api/config") resolves to the
	// same handler — both names refer to the operator's runtime config.
	api.HandleFunc("GET /api/config", s.api.HandleSettings)

	// WebSocket endpoint for real-time updates
	api.HandleFunc("GET /ws", s.hub.HandleWS)

	s.mux.Handle("/api/", requireToken(s.token, api))
	s.mux.Handle("/ws", requireToken(s.token, api))

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
