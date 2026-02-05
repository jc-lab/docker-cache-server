package lru_driver

import (
	"context"
	"io"

	"github.com/jc-lab/docker-cache-server/pkg/cache"

	"github.com/distribution/distribution/v3/registry/storage/driver"
	"github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
)

// Driver wraps a storage driver to track blob access for LRU eviction
type Driver struct {
	driver.StorageDriver
	tracker *cache.LRUTracker
	logger  *logrus.Logger
}

// New creates a new LRU tracking storage driver
func New(base driver.StorageDriver, tracker *cache.LRUTracker, logger *logrus.Logger) *Driver {
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	return &Driver{
		StorageDriver: base,
		tracker:       tracker,
		logger:        logger,
	}
}

// GetContent wraps the base driver's GetContent and tracks access
func (lru *Driver) GetContent(ctx context.Context, path string) ([]byte, error) {
	content, err := lru.StorageDriver.GetContent(ctx, path)
	if err != nil {
		return nil, err
	}

	// Track access if this is a blob data file
	if dgst := extractDigestFromPath(path); dgst != "" {
		if err := lru.tracker.RecordAccess(dgst, int64(len(content))); err != nil {
			lru.logger.Warnf("failed to record access for %s: %v", dgst, err)
		}
	}

	return content, nil
}

// Reader wraps the base driver's Reader and tracks access
func (lru *Driver) Reader(ctx context.Context, path string, offset int64) (io.ReadCloser, error) {
	reader, err := lru.StorageDriver.Reader(ctx, path, offset)
	if err != nil {
		return nil, err
	}

	// Track access if this is a blob data file
	if dgst := extractDigestFromPath(path); dgst != "" {
		// Get file info to track size
		if fi, err := lru.StorageDriver.Stat(ctx, path); err == nil {
			if err := lru.tracker.RecordAccess(dgst, fi.Size()); err != nil {
				lru.logger.Warnf("failed to record access for %s: %v", dgst, err)
			}
		}
	}

	return reader, nil
}

// Writer wraps the base driver's Writer to track writes
func (lru *Driver) Writer(ctx context.Context, path string, append bool) (driver.FileWriter, error) {
	lru.logger.Warnf("WRITER : %s | %v", path, append)

	writer, err := lru.StorageDriver.Writer(ctx, path, append)
	if err != nil {
		return nil, err
	}

	// Extract digest from path if available
	dgst := extractDigestFromPath(path)

	return &lruFileWriter{
		FileWriter: writer,
		tracker:    lru.tracker,
		digest:     dgst,
		path:       path,
		driver:     lru,
		ctx:        ctx,
		logger:     lru.logger,
	}, nil
}

func (lru *Driver) Move(ctx context.Context, sourcePath string, destPath string) error {
	lru.logger.Warnf("MOVE : %s -> %s", sourcePath, destPath)
	if err := lru.StorageDriver.Move(ctx, sourcePath, destPath); err != nil {
		return err
	}

	dgst := extractDigestFromPath(destPath)
	if dgst != "" {
		lru.recordWrite(ctx, destPath, dgst)
	}

	return nil
}

func (lru *Driver) recordWrite(ctx context.Context, path string, dgst digest.Digest) {
	// Get file size
	if fi, err := lru.Stat(ctx, path); err == nil {
		if err := lru.tracker.RecordWrite(dgst, fi.Size()); err != nil {
			lru.logger.Warnf("failed to record write for %s: %v", dgst, err)
		}
	}
}

// lruFileWriter wraps a FileWriter to track writes when committed
type lruFileWriter struct {
	driver.FileWriter
	tracker *cache.LRUTracker
	digest  digest.Digest
	path    string
	driver  *Driver
	ctx     context.Context
	logger  *logrus.Logger
}

// Commit wraps the base writer's Commit and tracks the write
func (w *lruFileWriter) Commit(ctx context.Context) error {
	if err := w.FileWriter.Commit(ctx); err != nil {
		return err
	}

	// Track write after successful commit
	if w.digest != "" {
		w.driver.recordWrite(ctx, w.path, w.digest)
	}

	return nil
}

// extractDigestFromPath extracts the digest from a blob storage path
// Blob paths typically look like: /docker/registry/v2/blobs/sha256/ab/abc123.../data
func extractDigestFromPath(path string) digest.Digest {
	// This is a simplified extraction - you may need to adjust based on actual path structure
	// The distribution library uses paths like: /docker/registry/v2/blobs/{algorithm}/{first2}/{digest}/data
	if len(path) < 20 {
		return ""
	}

	// Look for pattern like "sha256/ab/abc..." or "blobs/sha256/..."
	// We'll parse this more carefully
	parts := splitPath(path)
	for i, part := range parts {
		if part == "blobs" && i+3 < len(parts) {
			// Next should be algorithm (sha256), then first2, then full digest
			algorithm := parts[i+1]
			fullDigest := parts[i+3]
			dgstStr := algorithm + ":" + fullDigest
			if dgst, err := digest.Parse(dgstStr); err == nil {
				return dgst
			}
		}
	}

	return ""
}

// splitPath splits a path by '/' separator
func splitPath(path string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			if i > start {
				parts = append(parts, path[start:i])
			}
			start = i + 1
		}
	}
	if start < len(path) {
		parts = append(parts, path[start:])
	}
	return parts
}
