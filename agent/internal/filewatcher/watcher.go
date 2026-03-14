// SPDX-License-Identifier: LGPL-3.0-or-later
// Copyright (C) 2026 Vute Tech LTD
// Copyright (C) 2026 Bor contributors

// Package filewatcher monitors Bor-managed files and triggers a callback when
// they are modified or removed by an external process. The watcher watches
// the parent directories of managed files so that atomic renames (used by
// WriteFileAtomically) are detected correctly.
package filewatcher

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// FileWatcher monitors a dynamic set of file paths and invokes onTampered
// when one of them is modified or removed by an external process.
//
// The watcher suppresses events caused by Bor's own atomic writes:
// call Suppress before writing so that the resulting inotify events
// (a temp-file creation followed by a rename into place) are silently
// ignored for the configured duration.
type FileWatcher struct {
	watcher    *fsnotify.Watcher
	mu         sync.Mutex
	managed    map[string]bool      // clean absolute paths currently under management
	suppress   map[string]time.Time // ignore events for path until this time
	dirRefs    map[string]int       // directory watch reference counts
	onTampered func(path string)
}

// New creates a FileWatcher. onTampered is called in a new goroutine whenever
// a managed file is modified or removed by an external process.
func New(onTampered func(path string)) (*FileWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("filewatcher: %w", err)
	}
	return &FileWatcher{
		watcher:    w,
		managed:    make(map[string]bool),
		suppress:   make(map[string]time.Time),
		dirRefs:    make(map[string]int),
		onTampered: onTampered,
	}, nil
}

// SetManaged atomically replaces the current set of watched file paths.
// Parent directories are added to or removed from the OS watcher as needed.
// Empty strings in paths are silently ignored.
func (fw *FileWatcher) SetManaged(paths []string) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	desired := make(map[string]bool, len(paths))
	for _, p := range paths {
		if p != "" {
			desired[filepath.Clean(p)] = true
		}
	}

	// Unwatch directories that no longer have managed files.
	for p := range fw.managed {
		if !desired[p] {
			fw.unrefDir(filepath.Dir(p))
		}
	}
	// Watch directories for newly managed files.
	for p := range desired {
		if !fw.managed[p] {
			fw.refDir(filepath.Dir(p))
		}
	}
	fw.managed = desired
}

// Suppress instructs the watcher to ignore events for the given paths for
// duration d. Call this before any Bor-initiated file write to avoid
// re-triggering a restore from the agent's own writes.
// Empty strings in paths are silently ignored.
func (fw *FileWatcher) Suppress(paths []string, d time.Duration) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	until := time.Now().Add(d)
	for _, p := range paths {
		if p != "" {
			fw.suppress[filepath.Clean(p)] = until
		}
	}
}

// Run processes fsnotify events until ctx is cancelled.
// It should be started in a dedicated goroutine.
func (fw *FileWatcher) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-fw.watcher.Events:
			if !ok {
				return
			}
			fw.handleEvent(event)
		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("filewatcher: error: %v", err)
		}
	}
}

// Close releases the underlying OS watcher resources.
func (fw *FileWatcher) Close() error {
	return fw.watcher.Close()
}

func (fw *FileWatcher) refDir(dir string) {
	fw.dirRefs[dir]++
	if fw.dirRefs[dir] == 1 {
		if err := fw.watcher.Add(dir); err != nil {
			log.Printf("filewatcher: cannot watch directory %s: %v", dir, err)
		} else {
			log.Printf("filewatcher: watching directory %s", dir)
		}
	}
}

func (fw *FileWatcher) unrefDir(dir string) {
	fw.dirRefs[dir]--
	if fw.dirRefs[dir] <= 0 {
		delete(fw.dirRefs, dir)
		_ = fw.watcher.Remove(dir)
		log.Printf("filewatcher: stopped watching directory %s", dir)
	}
}

func (fw *FileWatcher) handleEvent(event fsnotify.Event) {
	// Only react to writes, atomic renames (Create = MOVED_TO), and removals.
	if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove) == 0 {
		return
	}

	path := filepath.Clean(event.Name)

	fw.mu.Lock()
	managed := fw.managed[path]
	until := fw.suppress[path]
	fw.mu.Unlock()

	if !managed {
		return
	}
	if time.Now().Before(until) {
		// Event is within the suppress window — originated from Bor itself.
		return
	}

	log.Printf("filewatcher: external modification detected: %s (%s)", path, event.Op)
	go fw.onTampered(path)
}
