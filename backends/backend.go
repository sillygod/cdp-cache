package backends

import (
	"errors"
	"io"
	"net/http"
)

// Backend is the cache storage backend.
type Backend interface {
	io.Writer
	io.Closer
	Clean() error
	Flush() error
	GetReader() (io.ReadCloser, error)
}

// Base wraps the http.ResponseWriter to match the Backend interface
type Base struct {
	w http.ResponseWriter
}

func (b *Base) Write(p []byte) (n int, err error) {
	return b.w.Write(p)
}

func (b *Base) Flush() error {
	if f, ok := b.w.(http.Flusher); ok {
		f.Flush()
	}

	return nil
}

func (b *Base) Clean() error {
	return nil
}

func (b *Base) Close() error {
	return nil
}

func (b *Base) GetReader() (io.ReadCloser, error) {
	return nil, errors.New("Private responses are no readable")
}

func WrapResponseWriterToBackend(w http.ResponseWriter) Backend {
	return &Base{
		w: w,
	}
}
