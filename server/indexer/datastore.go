// SPDX-License-Identifier: AGPL-3.0-or-later

package indexer

import (
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
)

const (
	dataDirName   = "data"
	htmlSubdir    = "html"
	faviconSubdir = "favicon"
)

// dataFilePath returns the filesystem path for a stored data file.
// The layout is: {dataDir}/{subdir}/{key[0:2]}/{key[2:4]}/{key[4:6]}/{key[6:]}.gz
func dataFilePath(dataDir, subdir, key string) string {
	return filepath.Join(dataDir, subdir, key[0:2], key[2:4], key[4:6], key[6:]+".gz")
}

// writeData compresses data with gzip and writes it to the data store.
// Files are named by their SHA-256 hash so identical content is stored only once.
// Returns the hex-encoded SHA-256 hash as the lookup key.
func writeData(dataDir, subdir string, data []byte) (string, error) {
	if len(data) == 0 {
		return "", nil
	}
	sum := sha256.Sum256(data)
	key := fmt.Sprintf("%x", sum)
	fpath := dataFilePath(dataDir, subdir, key)
	if _, err := os.Stat(fpath); err == nil {
		return key, nil
	}
	if err := os.MkdirAll(filepath.Dir(fpath), 0o755); err != nil {
		return "", fmt.Errorf("create data directory: %w", err)
	}
	f, err := os.Create(fpath)
	if err != nil {
		return "", fmt.Errorf("create data file: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			log.Warn().Err(cerr).Str("path", fpath).Msg("failed to close data file")
		}
	}()
	w := gzip.NewWriter(f)
	if _, err := w.Write(data); err != nil {
		return "", fmt.Errorf("write compressed data: %w", err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("flush compressed data: %w", err)
	}
	return key, nil
}

// readData reads and decompresses a stored data file identified by key.
func readData(dataDir, subdir, key string) ([]byte, error) {
	fpath := dataFilePath(dataDir, subdir, key)
	f, err := os.Open(fpath)
	if err != nil {
		return nil, fmt.Errorf("open data file: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			log.Warn().Err(cerr).Str("path", fpath).Msg("failed to close data file")
		}
	}()
	r, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("create gzip reader: %w", err)
	}
	defer func() {
		if cerr := r.Close(); cerr != nil {
			log.Warn().Err(cerr).Str("path", fpath).Msg("failed to close gzip reader")
		}
	}()
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("decompress data: %w", err)
	}
	return data, nil
}

// cleanupDataSubdir removes any .gz files under {dataDir}/{subdir} whose hash
// (filename without the .gz suffix) is not present in referenced.
func cleanupDataSubdir(dataDir, subdir string, referenced map[string]struct{}) error {
	root := filepath.Join(dataDir, subdir)
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil
	}
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Warn().Err(err).Str("path", path).Msg("error accessing data file during cleanup")
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".gz" {
			return nil
		}
		// Reconstruct the key from the 3-level directory prefix and the filename.
		// rel is like "aa/bb/cc/ddeeff....gz"
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		parts := splitPath(rel)
		if len(parts) != 4 {
			return nil
		}
		// parts[3] is the filename including .gz; strip the suffix to get the hash tail.
		key := parts[0] + parts[1] + parts[2] + parts[3][:len(parts[3])-3]
		if _, ok := referenced[key]; !ok {
			if rerr := os.Remove(path); rerr != nil {
				log.Warn().Err(rerr).Str("path", path).Msg("failed to remove orphaned data file")
			} else {
				log.Debug().Str("key", key).Str("subdir", subdir).Msg("removed orphaned data file")
			}
		}
		return nil
	})
}

// splitPath splits a filepath into its individual components.
func splitPath(p string) []string {
	var parts []string
	for {
		dir, file := filepath.Split(filepath.Clean(p))
		if file == "" || file == "." {
			break
		}
		parts = append([]string{file}, parts...)
		p = dir
		if dir == "" || dir == "." {
			break
		}
	}
	return parts
}
