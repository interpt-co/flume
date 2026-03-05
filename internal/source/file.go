package source

import (
	"context"
	"io"
	"path/filepath"
	"sync"
	"time"

	"github.com/nxadm/tail"
	"github.com/interpt-co/flume/internal/models"
)

// FileSource follows one or more files (like tail -f) and emits each new
// line as a LogMessage. It handles log rotation via the ReOpen option.
type FileSource struct {
	paths []string
}

// NewFileSource creates a new FileSource that follows the given file paths.
func NewFileSource(paths []string) *FileSource {
	return &FileSource{paths: paths}
}

// Name returns "file".
func (s *FileSource) Name() string {
	return "file"
}

// Start begins tailing all configured files. Lines from every file are
// merged into a single output channel. The channel is closed when the
// context is cancelled and all tail goroutines have finished.
func (s *FileSource) Start(ctx context.Context) (<-chan models.LogMessage, error) {
	ch := make(chan models.LogMessage)

	var wg sync.WaitGroup

	for _, p := range s.paths {
		t, err := tail.TailFile(p, tail.Config{
			Follow:    true,
			ReOpen:    true,
			Location:  &tail.SeekInfo{Offset: 0, Whence: io.SeekEnd},
			MustExist: true,
		})
		if err != nil {
			return nil, err
		}

		wg.Add(1)
		go func(t *tail.Tail, path string) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					t.Stop()
					t.Cleanup()
					return
				case line, ok := <-t.Lines:
					if !ok {
						return
					}
					if line.Err != nil {
						continue
					}
					msg := models.LogMessage{
						Content:   line.Text,
						Source:    models.SourceFile,
						Origin:    models.Origin{Name: filepath.Base(path), Meta: map[string]string{"path": path}},
						Timestamp: time.Now(),
					}
					select {
					case <-ctx.Done():
						t.Stop()
						t.Cleanup()
						return
					case ch <- msg:
					}
				}
			}
		}(t, p)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	return ch, nil
}
