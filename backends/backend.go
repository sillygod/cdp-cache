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
	Length() int
	Clean() error
	Flush() error
	GetReader() (io.ReadCloser, error)
}

// Base wraps the http.ResponseWriter to match the Backend interface
type Base struct {
	w http.ResponseWriter
}

// Write writes the content in the inner response
func (b *Base) Write(p []byte) (n int, err error) {
	return b.w.Write(p)
}

// Flush flushes buffered data to the client
func (b *Base) Flush() error {
	if f, ok := b.w.(http.Flusher); ok {
		f.Flush()
	}

	return nil
}

// Length base backend no length property
func (b *Base) Length() int {
	return 0
}

// Clean base backend no need to perform cleanup
func (b *Base) Clean() error {
	return nil
}

// Close base backend no need to release resource
func (b *Base) Close() error {
	return nil
}

// GetReader no reader for base backend
func (b *Base) GetReader() (io.ReadCloser, error) {
	return nil, errors.New("Private responses are not readable")
}

// WrapResponseWriterToBackend wrap the responseWriter to match the backend's interface
func WrapResponseWriterToBackend(w http.ResponseWriter) Backend {
	return &Base{
		w: w,
	}
}
