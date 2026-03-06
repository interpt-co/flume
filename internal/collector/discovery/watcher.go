package discovery

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
	log "github.com/sirupsen/logrus"
)

// FileEventType indicates whether a file was added or removed.
type FileEventType int

const (
	FileAdded   FileEventType = iota
	FileRemoved
)

// FileEvent is emitted when a container log file appears or disappears.
type FileEvent struct {
	Type FileEventType
	Path string
	Ref  ContainerRef
}

// WatchDir watches a directory for .log file creation and removal events.
// On startup it scans existing files and emits FileAdded for each.
// It blocks until ctx is cancelled.
func WatchDir(ctx context.Context, dir string) (<-chan FileEvent, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	// Verify directory is readable before starting.
	if _, err := os.ReadDir(dir); err != nil {
		watcher.Close()
		return nil, err
	}

	if err := watcher.Add(dir); err != nil {
		watcher.Close()
		return nil, err
	}

	ch := make(chan FileEvent, 256)

	go func() {
		defer watcher.Close()
		defer close(ch)

		// Initial scan inside goroutine to avoid blocking when >chan capacity files exist.
		entries, err := os.ReadDir(dir)
		if err != nil {
			log.WithError(err).Warn("discovery: initial scan failed")
			return
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			ref, err := ParseFilename(path)
			if err != nil {
				log.WithError(err).WithField("path", path).Debug("discovery: skipping file")
				continue
			}
			select {
			case ch <- FileEvent{Type: FileAdded, Path: path, Ref: ref}:
			case <-ctx.Done():
				return
			}
		}

		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if !strings.HasSuffix(event.Name, ".log") {
					continue
				}
				if event.Op&fsnotify.Create != 0 {
					ref, err := ParseFilename(event.Name)
					if err != nil {
						continue
					}
					select {
					case ch <- FileEvent{Type: FileAdded, Path: event.Name, Ref: ref}:
					case <-ctx.Done():
						return
					}
				}
				if event.Op&(fsnotify.Remove|fsnotify.Rename) != 0 {
					ref, err := ParseFilename(event.Name)
					if err != nil {
						continue
					}
					select {
					case ch <- FileEvent{Type: FileRemoved, Path: event.Name, Ref: ref}:
					case <-ctx.Done():
						return
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.WithError(err).Warn("discovery: fsnotify error")
			}
		}
	}()

	return ch, nil
}
