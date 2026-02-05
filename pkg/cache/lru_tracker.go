package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/sirupsen/logrus"
)

// BlobMeta holds metadata about a blob for LRU tracking
type BlobMeta struct {
	Digest       string    `json:"digest"`
	LastAccessed time.Time `json:"last_accessed"`
	Size         int64     `json:"size"`
	CreatedAt    time.Time `json:"created_at"`
}

// LRUTracker tracks blob access times for LRU eviction
type LRUTracker struct {
	mu          sync.RWMutex
	blobs       map[string]*BlobMeta
	metaDir     string
	ttl         time.Duration
	logger      *logrus.Logger
	stopCleanup chan struct{}
	wg          sync.WaitGroup
}

// NewLRUTracker creates a new LRU tracker
func NewLRUTracker(metaDir string, ttl time.Duration, logger *logrus.Logger) (*LRUTracker, error) {
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	// Ensure metadata directory exists
	if err := os.MkdirAll(metaDir, 0755); err != nil {
		return nil, fmt.Errorf("creating metadata directory: %w", err)
	}

	tracker := &LRUTracker{
		blobs:       make(map[string]*BlobMeta),
		metaDir:     metaDir,
		ttl:         ttl,
		logger:      logger,
		stopCleanup: make(chan struct{}),
	}

	// Load existing metadata
	if err := tracker.loadMetadata(); err != nil {
		logger.Warnf("failed to load metadata: %v", err)
	}

	return tracker, nil
}

// RecordAccess updates the last access time for a blob
func (t *LRUTracker) RecordAccess(dgst digest.Digest, size int64) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := dgst.String()
	now := time.Now()

	if meta, exists := t.blobs[key]; exists {
		meta.LastAccessed = now
	} else {
		t.blobs[key] = &BlobMeta{
			Digest:       key,
			LastAccessed: now,
			Size:         size,
			CreatedAt:    now,
		}
	}

	// Persist metadata asynchronously
	go t.saveMetadata(key)

	return nil
}

// RecordWrite records when a blob is written
func (t *LRUTracker) RecordWrite(dgst digest.Digest, size int64) error {
	return t.RecordAccess(dgst, size)
}

// GetExpiredBlobs returns blobs that have exceeded the TTL
func (t *LRUTracker) GetExpiredBlobs(ctx context.Context) []digest.Digest {
	t.mu.RLock()
	defer t.mu.RUnlock()

	now := time.Now()
	expired := []digest.Digest{}

	for key, meta := range t.blobs {
		if now.Sub(meta.LastAccessed) > t.ttl {
			if dgst, err := digest.Parse(key); err == nil {
				expired = append(expired, dgst)
			}
		}
	}

	t.logger.Infof("found %d expired blobs out of %d total", len(expired), len(t.blobs))
	return expired
}

// RemoveBlob removes a blob from tracking
func (t *LRUTracker) RemoveBlob(dgst digest.Digest) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := dgst.String()
	delete(t.blobs, key)

	// Remove metadata file
	metaFile := t.getMetaFilePath(key)
	if err := os.Remove(metaFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing metadata file: %w", err)
	}

	return nil
}

// StartCleanup starts the periodic cleanup goroutine
func (t *LRUTracker) StartCleanup(ctx context.Context, interval time.Duration, deleteFunc func(digest.Digest) error) {
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		t.logger.Infof("starting LRU cleanup with interval: %v, TTL: %v", interval, t.ttl)

		for {
			select {
			case <-ctx.Done():
				t.logger.Info("cleanup stopped due to context cancellation")
				return
			case <-t.stopCleanup:
				t.logger.Info("cleanup stopped")
				return
			case <-ticker.C:
				t.runCleanup(ctx, deleteFunc)
			}
		}
	}()
}

// runCleanup performs the cleanup of expired blobs
func (t *LRUTracker) runCleanup(ctx context.Context, deleteFunc func(digest.Digest) error) {
	t.logger.Info("running LRU cleanup")
	expired := t.GetExpiredBlobs(ctx)

	if len(expired) == 0 {
		t.logger.Debug("no expired blobs to clean up")
		return
	}

	deletedCount := 0
	var totalSize int64

	for _, dgst := range expired {
		if err := deleteFunc(dgst); err != nil {
			t.logger.Errorf("failed to delete blob %s: %v", dgst, err)
			continue
		}

		// Get size before removing
		t.mu.RLock()
		if meta, exists := t.blobs[dgst.String()]; exists {
			totalSize += meta.Size
		}
		t.mu.RUnlock()

		if err := t.RemoveBlob(dgst); err != nil {
			t.logger.Errorf("failed to remove blob metadata %s: %v", dgst, err)
		}

		deletedCount++
	}

	t.logger.Infof("cleanup completed: deleted %d blobs, freed %d bytes", deletedCount, totalSize)
}

// StopCleanup stops the cleanup goroutine
func (t *LRUTracker) StopCleanup() {
	close(t.stopCleanup)
	t.wg.Wait()
}

// loadMetadata loads metadata from disk
func (t *LRUTracker) loadMetadata() error {
	entries, err := os.ReadDir(t.metaDir)
	if err != nil {
		return fmt.Errorf("reading metadata directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		metaFile := filepath.Join(t.metaDir, entry.Name())
		data, err := os.ReadFile(metaFile)
		if err != nil {
			t.logger.Warnf("failed to read metadata file %s: %v", metaFile, err)
			continue
		}

		var meta BlobMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			t.logger.Warnf("failed to unmarshal metadata file %s: %v", metaFile, err)
			continue
		}

		t.blobs[meta.Digest] = &meta
	}

	t.logger.Infof("loaded %d blob metadata entries", len(t.blobs))
	return nil
}

// saveMetadata saves metadata for a specific blob to disk
func (t *LRUTracker) saveMetadata(key string) {
	t.mu.RLock()
	meta, exists := t.blobs[key]
	t.mu.RUnlock()

	if !exists {
		return
	}

	metaFile := t.getMetaFilePath(key)
	data, err := json.Marshal(meta)
	if err != nil {
		t.logger.Errorf("failed to marshal metadata for %s: %v", key, err)
		return
	}

	// Ensure directory exists
	dir := filepath.Dir(metaFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.logger.Errorf("failed to create metadata directory for %s: %v", key, err)
		return
	}

	if err := os.WriteFile(metaFile, data, 0644); err != nil {
		t.logger.Errorf("failed to write metadata file %s: %v", metaFile, err)
	}
}

// getMetaFilePath returns the path to the metadata file for a digest
func (t *LRUTracker) getMetaFilePath(key string) string {
	// Create subdirectories based on first few characters to avoid too many files in one directory
	if len(key) > 10 {
		return filepath.Join(t.metaDir, key[:2], key[2:4], key+".json")
	}
	return filepath.Join(t.metaDir, key+".json")
}

// GetStats returns statistics about tracked blobs
func (t *LRUTracker) GetStats() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var totalSize int64
	for _, meta := range t.blobs {
		totalSize += meta.Size
	}

	return map[string]interface{}{
		"total_blobs": len(t.blobs),
		"total_size":  totalSize,
		"ttl":         t.ttl.String(),
	}
}
