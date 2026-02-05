package main

import (
	"fmt"
	"os"

	"github.com/jc-lab/docker-cache-server/pkg/config"
	"github.com/jc-lab/docker-cache-server/pkg/server"

	"github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
)

func main() {
	// Setup flags
	flags := pflag.NewFlagSet("docker-cache-server", pflag.ExitOnError)
	config.BindFlags(flags)

	version := flags.Bool("version", false, "Print version and exit")

	if err := flags.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing flags: %v\n", err)
		os.Exit(1)
	}

	// Print version
	if *version {
		fmt.Println("docker-cache-server v1.0.0")
		os.Exit(0)
	}

	configFile, err := flags.GetString("config")
	if err != nil {
		logrus.Fatal(err)
	}

	// Load configuration
	cfg, err := config.Load(configFile, flags)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading configuration: %v\n", err)
		os.Exit(1)
	}

	// Setup logger
	logger := logrus.New()
	logger.SetOutput(os.Stdout)
	logger.SetLevel(logrus.InfoLevel)
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	// Create and start server
	srv, err := server.New(&server.Options{
		Config: cfg,
		Logger: logger,
	})
	if err != nil {
		logger.Fatalf("Failed to create server: %v", err)
	}

	logger.Info("Docker Cache Server starting...")
	if err := srv.Start(); err != nil {
		logger.Fatalf("Server error: %v", err)
	}
}
