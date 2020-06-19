package httpcache

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/sillygod/cdp-cache/backends"
)

// TODO: caddy2 provide a copyheader function. Maybe, we
// can use that instread
func copyHeaders(from http.Header, to http.Header) {
	for k, values := range from {
		for _, v := range values {
			to.Add(k, v)
		}
	}
}

// Response encapsulates the entry
type Response struct {
	Code       int
	HeaderMap  http.Header
	body       backends.Backend
	snapHeader http.Header

	wroteHeader  bool
	bodyComplete bool

	bodyChan         chan struct{} // indicates whether the backend is set or not.
	bodyCompleteChan chan struct{}
	closedChan       chan struct{}
	headersChan      chan struct{}
}

// NewResponse returns an initialized Response.
func NewResponse() *Response {
	r := &Response{
		Code:             200,
		HeaderMap:        http.Header{},
		body:             nil,
		bodyChan:         make(chan struct{}, 1),
		closedChan:       make(chan struct{}, 1),
		headersChan:      make(chan struct{}, 1),
		bodyCompleteChan: make(chan struct{}, 1),
	}
	return r
}

// Header return the header from the upstream response
func (r *Response) Header() http.Header {
	return r.HeaderMap
}

func (r *Response) writeHeader(b []byte, str string) {
	if r.wroteHeader {
		return
	}

	if len(str) > 512 {
		str = str[:512]
	}

	h := r.Header()
	_, hasType := h["Content-Type"]
	hasTE := h.Get("Transfer-Encoding") != ""

	if !hasType && !hasTE {
		if b == nil {
			b = []byte(str)
		}

		if len(b) > 512 {
			b = b[:512]
		}

		h.Set("Content-Type", http.DetectContentType(b))
	}

	r.WriteHeader(200)
}

// Write writes the upstream's content in the backend's storage
func (r *Response) Write(buf []byte) (int, error) {

	if !r.wroteHeader {
		r.writeHeader(buf, "")
	}

	if r.body == nil {
		<-r.bodyChan
	}

	if r.body != nil {
		return r.body.Write(buf)
	}

	return 0, errors.New("No storage provided")
}

// WaitClose waits the response to be closed.
func (r *Response) WaitClose() {
	<-r.closedChan
}

// GetReader gets the reader from the setted backend
func (r *Response) GetReader() (io.ReadCloser, error) {
	if r.bodyComplete == false {
		<-r.bodyCompleteChan
	}
	return r.body.GetReader()
}

// SetBody sets the backend to body for the further write usage
func (r *Response) SetBody(body backends.Backend) {
	r.body = body
	r.bodyChan <- struct{}{}
}

// WaitHeaders waits the header to be written
func (r *Response) WaitHeaders() {
	<-r.headersChan
}

// WriteHeader keeps the upstream response header
func (r *Response) WriteHeader(code int) {
	if r.wroteHeader {
		return
	}

	r.Code = code
	r.wroteHeader = true

	r.snapHeader = http.Header{}
	copyHeaders(r.Header(), r.snapHeader)
	r.snapHeader.Del("server")

	// NOTE: intent to delete the content-length
	r.snapHeader.Del("Content-Length")
	r.headersChan <- struct{}{}
}

func shouldUseCache(req *http.Request) bool {
	// TODO Add more logic like get params, ?nocache=true

	if req.Method != "GET" && req.Method != "HEAD" {
		// Only cache Get and head request
		return false
	}

	// Range requests still not supported
	// It may happen that the previous request for this url has a successful response
	// but for another Range. So a special handling is needed
	if req.Header.Get("range") != "" {
		return false
	}

	if isWebSocket(req.Header) {
		return false
	}

	return true

}

func isWebSocket(h http.Header) bool {
	if h == nil {
		return false
	}

	// Get gets the *first* value associated with the given key.
	if strings.ToLower(h.Get("Upgrade")) != "websocket" {
		return false
	}

	// To access multiple values of a key, access the map directly.
	for _, value := range h.Values("Connection") {
		if strings.ToLower(value) == "websocket" {
			return true
		}
	}

	return false
}

// Flush flushes the backend's storage (currently, only file storage need to call this)
func (r *Response) Flush() {
	if !r.wroteHeader {
		r.WriteHeader(200)
	}

	if r.body == nil {
		return
	}

	r.body.Flush()
}

// Close indicate the data is completely written to the body
// so that we can close it.
func (r *Response) Close() error {
	if r.body == nil {
		<-r.bodyChan
	}

	r.body.Close()

	r.bodyComplete = true
	r.bodyCompleteChan <- struct{}{}
	r.closedChan <- struct{}{}

	return nil
}

// Clean performs purge the cache
func (r *Response) Clean() error {

	if r.body == nil {
		return nil
	}

	return r.body.Clean()
}
