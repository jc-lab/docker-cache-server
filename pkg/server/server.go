package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	auth2 "github.com/distribution/distribution/v3/registry/auth"
	"github.com/distribution/distribution/v3/registry/storage/driver/filesystem"
	"github.com/docker/go-metrics"
	"github.com/gorilla/mux"
	"github.com/jc-lab/docker-cache-server/internal/handlers"
	"github.com/jc-lab/docker-cache-server/pkg/auth/silly"
	"github.com/jc-lab/docker-cache-server/pkg/auth/userpass"
	"github.com/jc-lab/docker-cache-server/pkg/cache"
	"github.com/jc-lab/docker-cache-server/pkg/config"
	"github.com/jc-lab/docker-cache-server/pkg/lru_driver"
	"github.com/sirupsen/logrus"
)

// CacheServer is the main server interface that can be embedded in other applications
type CacheServer interface {
	// Start starts the server (blocking)
	Start() error

	// Shutdown gracefully shuts down the server
	Shutdown(timeout time.Duration) error

	// Config returns the current configuration
	Config() *config.Config

	// Stats returns cache statistics
	Stats() map[string]interface{}
}

// Options for creating a new server
type Options struct {
	// Config is the server configuration
	Config *config.Config

	// Logger is the logger to use (if nil, creates a new one)
	Logger *logrus.Logger

	// AuthValidator is a custom authentication validator (optional)
	// If provided, overrides the default basic auth
	AuthValidator userpass.AuthenticateFunc

	// OnBlobAccess is called when a blob is accessed (optional)
	OnBlobAccess func(digest string, size int64)

	// OnBlobDelete is called when a blob is deleted (optional)
	OnBlobDelete func(digest string)
}

// cacheServer implements CacheServer
type cacheServer struct {
	config *config.Config

	appContext context.Context
	appCancel  context.CancelFunc

	tracker    *cache.LRUTracker
	logger     *logrus.Logger
	opts       *Options
	handler    *handlers.App
	httpServer *http.Server

	debugServer *http.Server
	debugMux    *mux.Router
}

const authRelam = "docker-cache-server"
const authService = "registry"

// New creates a new cache server instance
func New(opts *Options) (CacheServer, error) {
	if opts == nil {
		return nil, fmt.Errorf("options cannot be nil")
	}

	if opts.Config == nil {
		opts.Config = config.DefaultConfig()
	}

	logger := opts.Logger
	if logger == nil {
		logger = logrus.New()
		logger.SetLevel(logrus.InfoLevel)
		logger.SetFormatter(&logrus.TextFormatter{
			FullTimestamp: true,
		})
	}

	var err error
	var accessController auth2.AccessController
	if !opts.Config.Auth.Enabled {
		accessController = silly.MustNew(authRelam, authService)
	} else if opts.AuthValidator != nil {
		accessController, err = userpass.NewWithCallback(authRelam, opts.AuthValidator)
	} else {
		accessController, err = userpass.NewWithCreds(authRelam, opts.Config.Auth.Users)
	}
	if err != nil {
		return nil, err
	}

	metaCacheDir := filepath.Join(opts.Config.Storage.Directory, "meta/cache")
	repoDir := filepath.Join(opts.Config.Storage.Directory, "data")

	_ = os.MkdirAll(metaCacheDir, 0755)
	_ = os.MkdirAll(repoDir, 0755)

	fsDriver := filesystem.New(filesystem.DriverParameters{
		RootDirectory: repoDir,
		MaxThreads:    100,
	})
	lruTracker, err := cache.NewLRUTracker(metaCacheDir, opts.Config.Cache.TTL, logger)
	storageDriver := lru_driver.New(fsDriver, lruTracker, logger)

	server := &cacheServer{
		config: opts.Config,
		logger: logger,
	}
	server.appContext, server.appCancel = context.WithCancel(context.Background())
	server.handler, err = handlers.NewApp(server.appContext, &handlers.Config{
		HttpPrefix:       opts.Config.Http.Prefix,
		HttpHost:         opts.Config.Http.Host,
		HttpRelativeURLs: opts.Config.Http.Relativeurls,
		AccessController: accessController,
		Driver:           storageDriver,
	})

	// Create HTTP server
	server.httpServer = &http.Server{
		Addr:         opts.Config.Http.Addr,
		Handler:      server.handler,
		ReadTimeout:  300 * time.Second,
		WriteTimeout: 300 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	if opts.Config.Http.Debug.Addr != "" {
		debugRouter := mux.NewRouter()
		server.debugMux = debugRouter.PathPrefix("/debug/").Subrouter()
		server.debugServer = &http.Server{
			Addr:         opts.Config.Http.Debug.Addr,
			Handler:      debugRouter,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 5 * time.Second,
			IdleTimeout:  120 * time.Second,
		}

		server.debugMux.Path("/health").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		if prom := opts.Config.Http.Debug.Prometheus; prom.Enabled {
			logger.Info("providing prometheus metrics on ", prom.Path)
			server.debugMux.PathPrefix(prom.Path).Handler(metrics.Handler())
		}
	}

	return server, nil
}

// Start starts the server and blocks until shutdown
func (s *cacheServer) Start() error {
	s.logger.Infof("starting Docker cache server (%s)", s.httpServer.Addr)

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Start server in goroutine
	errChan := make(chan error, 1)
	if s.debugServer != nil {
		s.logger.Infof("starting debug server (%s)", s.debugServer.Addr)
		go func() {
			if err := s.debugServer.ListenAndServe(); err != nil {
				s.logger.Errorf("error starting debug server: %v", err)
			}
		}()
	}
	go func() {
		errChan <- s.httpServer.ListenAndServe()
	}()

	// Wait for shutdown signal or error
	select {
	case err := <-errChan:
		return err
	case sig := <-sigChan:
		s.logger.Infof("received signal: %v", sig)
		return s.Shutdown(30 * time.Second)
	}
}

// Shutdown gracefully shuts down the server
func (s *cacheServer) Shutdown(timeout time.Duration) error {
	var wg sync.WaitGroup
	var errorMu sync.Mutex
	var errorList []error

	s.logger.Info("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := s.httpServer.Shutdown(ctx); err != nil {
			errorMu.Lock()
			errorList = append(errorList, err)
			errorMu.Unlock()
		}
	}()
	go func() {
		defer wg.Done()
		if err := s.handler.Shutdown(); err != nil {
			errorMu.Lock()
			errorList = append(errorList, err)
			errorMu.Unlock()
		}
	}()
	wg.Wait()
	if len(errorList) > 0 {
		return errors.Join(errorList...)
	}
	return nil
}

// Config returns the server configuration
func (s *cacheServer) Config() *config.Config {
	return s.config
}

// Stats returns cache statistics
func (s *cacheServer) Stats() map[string]interface{} {
	// This would need the tracker to be accessible
	// For now return basic stats
	return map[string]interface{}{
		"ttl":              s.config.Cache.TTL.String(),
		"cleanup_interval": s.config.Cache.CleanupInterval.String(),
		"storage_dir":      s.config.Storage.Directory,
	}
}

// RunWithContext runs the server with a custom context
func RunWithContext(ctx context.Context, opts *Options) error {
	server, err := New(opts)
	if err != nil {
		return err
	}

	// Start server in goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Start()
	}()

	// Wait for context cancellation or error
	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return server.Shutdown(30 * time.Second)
	}
}

// ListenAndServe is a convenience function that creates and starts a server
func ListenAndServe(cfg *config.Config) error {
	server, err := New(&Options{
		Config: cfg,
	})
	if err != nil {
		return err
	}

	return server.Start()
}

// ServeHTTP allows embedding the cache server as an http.Handler
type Handler struct {
	server CacheServer
}
