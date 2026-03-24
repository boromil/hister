package indexer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/rs/zerolog/log"

	"github.com/asciimoo/hister/config"
	"github.com/asciimoo/hister/files"
	"github.com/asciimoo/hister/server/document"
	"github.com/asciimoo/hister/server/model"
)

var (
	ErrEmptyFile      = errors.New("empty file")
	ErrBinaryFile     = errors.New("binary file")
	ErrFileTooLarge   = errors.New("file too large")
	ErrReadFile       = errors.New("cannot read file")
	ErrAlreadyIndexed = errors.New("already indexed")

	maxFileSize int64 = 1024 * 1024 // 1MB default
)

const indexBatchSize = 50

type ReadFileError struct {
	Msg string
}

func (e *ReadFileError) Unwrap() error {
	return ErrReadFile
}

func (e *ReadFileError) Error() string {
	return fmt.Sprintf("%s: %s", ErrReadFile.Error(), e.Msg)
}

func IndexAll(ctx context.Context, dirs []*config.Directory, workers int) error {
	if workers < 1 {
		workers = 1
	}
	for _, dir := range dirs {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		expanded := files.ExpandHome(dir.Path)
		if err := indexDirectory(ctx, expanded, dir, workers); err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			log.Error().Err(err).Str("directory", expanded).Msg("Failed to index directory")
		}
	}
	return nil
}

func indexDirectory(ctx context.Context, dir string, cfg *config.Directory, workers int) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("cannot access directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", dir)
	}

	var userID uint
	if cfg.User != "" {
		u, err := model.GetUser(cfg.User)
		if err != nil {
			log.Error().Err(err).Str("directory", dir).Msg("Failed to resolve user for directory")
			return fmt.Errorf("user %q not found for directory %s: %w", cfg.User, dir, err)
		}
		userID = u.ID
	}

	log.Debug().Str("directory", dir).Msg("Indexing directory")

	sem := make(chan struct{}, workers)

	var (
		mu           sync.Mutex
		batch        = NewMultiBatch()
		indexed      int
		skipped      int
		pendingFlush bool
		wg           sync.WaitGroup
		walkErr      error
	)

	// Must be called with mu held.
	flushBatch := func() error {
		err := batch.Save()
		batch = NewMultiBatch()
		pendingFlush = false
		return err
	}

	walkErr = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err != nil {
			log.Warn().Err(err).Str("path", path).Msg("Error accessing path")
			return nil
		}
		if d.IsDir() {
			if path != dir && files.ShouldSkipDir(d.Name(), cfg.Excludes, cfg.IncludeHidden) {
				return filepath.SkipDir
			}
			return nil
		}
		if !cfg.IsMatching(d.Name()) {
			return nil
		}

		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}

		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			defer func() { <-sem }()

			doc, err := readFileDoc(p, userID)
			if err != nil {
				log.Debug().Err(err).Str("path", p).Msg("Skipping file")
				mu.Lock()
				skipped++
				mu.Unlock()
				return
			}

			mu.Lock()
			defer mu.Unlock()

			if err := batch.Add(doc); err != nil {
				log.Warn().Err(err).Str("path", p).Msg("Failed to add file to batch")
				skipped++
				return
			}
			indexed++
			pendingFlush = true
			if indexed%indexBatchSize == 0 {
				if err := flushBatch(); err != nil {
					log.Warn().Err(err).Msg("Failed to flush index batch")
				}
			}
		}(path)

		return nil
	})

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()

	if pendingFlush {
		if err := flushBatch(); err != nil {
			log.Warn().Err(err).Msg("Failed to flush final index batch")
		}
	}

	log.Debug().Str("directory", dir).Int("indexed", indexed).Int("skipped", skipped).Msg("Directory indexing complete")
	return walkErr
}

func readFileDoc(path string, userID uint) (*document.Document, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() == 0 {
		return nil, ErrEmptyFile
	}

	if info.Size() > maxFileSize {
		return nil, fmt.Errorf("%w: %d bytes", ErrFileTooLarge, info.Size())
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	fileURL := files.PathToFileURL(absPath)

	existing := GetByURLAndUser(fileURL, userID)
	if existing != nil && existing.Added == info.ModTime().Unix() {
		return nil, ErrAlreadyIndexed
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, &ReadFileError{Msg: err.Error()}
	}

	doc := &document.Document{
		URL:    fileURL,
		Added:  info.ModTime().Unix(),
		UserID: userID,
	}

	if strings.EqualFold(filepath.Ext(path), ".pdf") {
		text, err := extractPDFText(content)
		if err != nil {
			return nil, fmt.Errorf("pdf text extraction: %w", err)
		}
		if strings.TrimSpace(text) == "" {
			return nil, errors.New("pdf contains no extractable text")
		}
		doc.Text = text
		doc.AddMetadata("type", "pdf")
		return doc, nil
	}

	if !utf8.Valid(content) {
		return nil, ErrBinaryFile
	}
	if int64(len(content)) > maxFileSize {
		return nil, fmt.Errorf("%w: %d bytes", ErrFileTooLarge, int64(len(content)))
	}

	return &document.Document{
		URL:    fileURL,
		Text:   string(content),
		Added:  info.ModTime().Unix(),
		UserID: userID,
	}, nil
}

// IndexFile indexes a single file. Used by the file watcher.
func IndexFile(path string, userID uint) error {
	doc, err := readFileDoc(path, userID)
	if err != nil {
		if errors.Is(err, ErrAlreadyIndexed) {
			return nil
		}
		return err
	}
	return i.AddDocument(doc)
}

// DeleteFile removes the document for the given filesystem path from the index.
// It uses a url: field query so it removes the file across all users and
// language-specific sub-indexes. Returns nil if the document is not found.
func DeleteFile(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}
	fileURL := files.PathToFileURL(absPath)
	_, err = DeleteByQuery("url:"+fileURL, nil, nil)
	return err
}
