package httpcache

import (
	"errors"
	"net/http"
	"strings"
	"sync"

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

// NewResponse returns an initialized Response.
type Response struct {
	Code       int
	HeaderMap  http.Header
	body       backends.Backend
	snapHeader http.Header

	wroteHeader   bool
	firstByteSent bool

	bodyLock    *sync.RWMutex
	closedLock  *sync.RWMutex
	headersLock *sync.RWMutex

	closeNotify chan bool
}

// NewResponse returns an initialized Response.
func NewResponse() *Response {
	r := &Response{
		Code:        200,
		HeaderMap:   http.Header{},
		body:        nil,
		closeNotify: make(chan bool, 1),
		bodyLock:    new(sync.RWMutex),
		closedLock:  new(sync.RWMutex),
		headersLock: new(sync.RWMutex),
	}

	r.bodyLock.Lock()
	r.closedLock.Lock()
	r.headersLock.Lock()

	return r
}

func (r *Response) Header() http.Header {
	return r.HeaderMap
}

func (r *Response) CloseNotify() <-chan bool {
	return r.closeNotify
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

func (r *Response) Write(buf []byte) (int, error) {
	if !r.wroteHeader {
		r.writeHeader(buf, "")
	}

	if !r.firstByteSent {
		r.firstByteSent = true
		r.WaitBody()
	}

	if r.body != nil {
		return r.body.Write(buf)
	}

	return 0, errors.New("No storage provided")
}

func (r *Response) WaitClose() {
	r.closedLock.RLock()
}

func (r *Response) WaitBody() {
	r.bodyLock.RLock()
}

func (r *Response) SetBody(body backends.Backend) {
	r.body = body
	r.bodyLock.Unlock()
}

func (r *Response) WaitHeaders() {
	r.headersLock.RLock()
}

func (r *Response) WriteHeader(code int) {
	if r.wroteHeader {
		return
	}

	r.Code = code
	r.wroteHeader = true

	r.snapHeader = http.Header{}
	copyHeaders(r.Header(), r.snapHeader)
	r.snapHeader.Del("server")
	r.headersLock.Unlock()
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

func (r *Response) Flush() {
	if !r.wroteHeader {
		r.WriteHeader(200)
	}

	if r.body == nil {
		return
	}

	r.body.Flush()
}

func (r *Response) Close() error {
	defer r.closedLock.Unlock()

	if r.body != nil {
		return r.body.Close()
	}
	return nil
}

// Clean the body if it is set
func (r *Response) Clean() error {
	r.bodyLock.RLock()
	defer r.bodyLock.RUnlock()

	if r.body == nil {
		return nil
	}

	return r.body.Clean()
}
