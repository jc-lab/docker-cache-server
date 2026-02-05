package main

import (
	"context"
	"log"
	"time"

	"github.com/jc-lab/docker-cache-server/pkg/config"
	"github.com/jc-lab/docker-cache-server/pkg/server"
	"github.com/sirupsen/logrus"
)

func main() {
	// Example 1: Using with default configuration
	exampleBasicUsage()

	// Example 2: Using with custom configuration
	exampleCustomConfig()

	// Example 3: Using as a library with custom auth
	exampleCustomAuth()

	// Example 4: Running with context
	exampleWithContext()
}

func exampleBasicUsage() {
	// Use default configuration
	cfg := config.DefaultConfig()

	// Start server (blocking)
	if err := server.ListenAndServe(cfg); err != nil {
		log.Fatalf("Http error: %v", err)
	}
}

func exampleCustomConfig() {
	// Create custom configuration
	cfg := &config.Config{
		Http: config.HttpConfig{
			Addr: "0.0.0.0",
			Port: 5000,
		},
		Storage: config.StorageConfig{
			Directory: "/custom/cache/dir",
		},
		Auth: config.AuthConfig{
			Enabled: true,
			Users: []config.UserCreds{
				{Username: "admin", Password: "secret123"},
				{Username: "readonly", Password: "readonly123"},
			},
		},
		Cache: config.CacheConfig{
			TTL:             7 * 24 * time.Hour, // 7 days
			CleanupInterval: 30 * time.Minute,   // 30 minutes
		},
	}

	srv, err := server.New(&server.Options{
		Config: cfg,
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := srv.Start(); err != nil {
		log.Fatal(err)
	}
}

func exampleCustomAuth() {
	cfg := config.DefaultConfig()

	// Custom logger
	logger := logrus.New()
	logger.SetLevel(logrus.DebugLevel)

	srv, err := server.New(&server.Options{
		Config: cfg,
		Logger: logger,
		// Custom authentication validator
		AuthValidator: func(username, password string) bool {
			// Example: validate against external service
			// return externalAuthService.Validate(username, password)
			return username == "custom" && password == "pass"
		},
		// Callback when blob is accessed
		OnBlobAccess: func(digest string, size int64) {
			logger.Infof("Blob accessed: %s (size: %d bytes)", digest, size)
		},
		// Callback when blob is deleted
		OnBlobDelete: func(digest string) {
			logger.Infof("Blob deleted: %s", digest)
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := srv.Start(); err != nil {
		log.Fatal(err)
	}
}

func exampleWithContext() {
	cfg := config.DefaultConfig()
	cfg.Http.Port = 5001

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Hour)
	defer cancel()

	// Run server with context - will stop when context is cancelled
	if err := server.RunWithContext(ctx, &server.Options{
		Config: cfg,
	}); err != nil {
		log.Fatal(err)
	}
}
