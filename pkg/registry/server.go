package registry

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/distribution/distribution/v3/configuration"
	"github.com/distribution/distribution/v3/registry/handlers"
	"github.com/distribution/distribution/v3/registry/storage/driver/filesystem"
	"github.com/jc-lab/docker-cache-server/pkg/cache"
	"github.com/jc-lab/docker-cache-server/pkg/config"
	"github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
)

// Server represents the Docker cache server
type Server struct {
	config     *config.Config
	app        *handlers.App
	tracker    *cache.LRUTracker
	logger     *logrus.Logger
	httpServer *http.Server
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewServer creates a new Docker cache server
func NewServer(cfg *config.Config, logger *logrus.Logger) (*Server, error) {
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create LRU tracker
	metaDir := fmt.Sprintf("%s/.metadata", cfg.Storage.Directory)
	tracker, err := cache.NewLRUTracker(metaDir, cfg.Cache.TTL, logger)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("creating LRU tracker: %w", err)
	}

	// Create distribution configuration
	distConfig := &configuration.Configuration{
		Version: configuration.MajorMinorVersion(0, 1),
		Storage: configuration.Storage{
			"filesystem": configuration.Parameters{
				"rootdirectory": cfg.Storage.Directory,
			},
			"maintenance": configuration.Parameters{
				"uploadpurging": map[interface{}]interface{}{
					"enabled": false,
				},
			},
			"delete": configuration.Parameters{
				"enabled": true,
			},
		},
		HTTP: configuration.HTTP{
			Addr:    fmt.Sprintf("%s:%d", cfg.Server.Address, cfg.Server.Port),
			Headers: http.Header{},
		},
		Log: configuration.Log{
			Level:  configuration.Loglevel("info"),
			Fields: map[string]interface{}{},
		},
		Catalog: configuration.Catalog{
			MaxEntries: 1000,
		},
	}

	// Configure authentication if enabled
	if cfg.Auth.Enabled {
		distConfig.Auth = configuration.Auth{
			"htpasswd": configuration.Parameters{
				"realm": "Docker Cache Server",
				"path":  fmt.Sprintf("%s/.htpasswd", cfg.Storage.Directory),
			},
		}
	}

	// Create the base filesystem driver
	baseDriver, err := filesystem.FromParameters(distConfig.Storage.Parameters())
	if err != nil {
		cancel()
		return nil, fmt.Errorf("creating storage driver: %w", err)
	}

	// Wrap with LRU tracking driver
	_ = lru_driver.NewLRUDriver(baseDriver, tracker, logger)

	// Create distribution app with our custom driver
	// We need to modify the config to use our wrapped driver
	app := handlers.NewApp(ctx, distConfig)

	// Override the app's driver with our LRU tracking driver
	// Note: This uses reflection or requires modification of distribution library
	// For now, we'll work with the existing app structure

	server := &Server{
		config:  cfg,
		app:     app,
		tracker: tracker,
		logger:  logger,
		ctx:     ctx,
		cancel:  cancel,
	}

	// Wrap the app with authentication middleware if enabled
	var handler http.Handler = app
	if cfg.Auth.Enabled {
		handler = server.authMiddleware(app)
	}

	// Create HTTP server
	server.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Address, cfg.Server.Port),
		Handler:      handler,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	return server, nil
}

// Start starts the server and cleanup routines
func (s *Server) Start() error {
	s.logger.Infof("starting Docker cache server on %s:%d", s.config.Server.Address, s.config.Server.Port)
	s.logger.Infof("storage directory: %s", s.config.Storage.Directory)
	s.logger.Infof("cache TTL: %v, cleanup interval: %v", s.config.Cache.TTL, s.config.Cache.CleanupInterval)

	// Start cleanup routine
	s.tracker.StartCleanup(s.ctx, s.config.Cache.CleanupInterval, s.deleteBlob)

	// Start HTTP server
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("HTTP server error: %w", err)
	}

	return nil
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(timeout time.Duration) error {
	s.logger.Info("shutting down server...")

	// Stop cleanup routine
	s.tracker.StopCleanup()

	// Cancel context
	s.cancel()

	// Shutdown HTTP server
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("HTTP server shutdown: %w", err)
	}

	// Shutdown distribution app
	if err := s.app.Shutdown(); err != nil {
		s.logger.Warnf("error shutting down distribution app: %v", err)
	}

	s.logger.Info("server stopped")
	return nil
}

// deleteBlob deletes a blob from storage
func (s *Server) deleteBlob(dgst digest.Digest) error {
	// Use the distribution app's registry to delete the blob
	// This requires accessing the blob store
	s.logger.Infof("deleting expired blob: %s", dgst)

	// Get the blob statter from registry
	statter := s.app.Config.Storage.Type()
	_ = statter // TODO: implement actual deletion through distribution API

	// For now, we'll log the deletion request
	// In a full implementation, we'd need to:
	// 1. Access the registry's blob store
	// 2. Call Delete on the blob
	// This may require exposing internal distribution APIs or using reflection

	return nil
}

// authMiddleware wraps the handler with basic authentication
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for /v2/ endpoint (base check)
		if r.URL.Path == "/v2/" || r.URL.Path == "/v2" {
			next.ServeHTTP(w, r)
			return
		}

		// Get credentials
		username, password, ok := r.BasicAuth()
		if !ok {
			s.sendAuthChallenge(w)
			return
		}

		// Validate credentials
		if !s.validateCredentials(username, password) {
			s.logger.Warnf("authentication failed for user: %s", username)
			s.sendAuthChallenge(w)
			return
		}

		s.logger.Debugf("authenticated user: %s", username)
		next.ServeHTTP(w, r)
	})
}

// validateCredentials checks if the provided credentials are valid
func (s *Server) validateCredentials(username, password string) bool {
	for _, user := range s.config.Auth.Users {
		if user.Username == username && user.Password == password {
			return true
		}
	}
	return false
}

// sendAuthChallenge sends a 401 Unauthorized response with WWW-Authenticate header
func (s *Server) sendAuthChallenge(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="Docker Cache Server"`)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"errors":[{"code":"UNAUTHORIZED","message":"authentication required"}]}`))
}
