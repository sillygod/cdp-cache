package httpcache

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/caddyserver/caddy/v2"
	"github.com/sillygod/cdp-cache/pkg/helper"
)

var (
	bufPool = sync.Pool{
		New: func() interface{} {
			return new(bytes.Buffer)
		},
	}

	purgeRepl *caddy.Replacer
)

func init() {

	purgeRepl = caddy.NewReplacer()
	caddy.RegisterModule(cachePurge{})
}

// cachePurge is a module that provides the /purge endpoint as the admin api.
type cachePurge struct{}

// PurgePayload holds the field which will be unmarshalled from the request's body
// NOTE: the format of URI can contains the query param.
// ex. when the client send a delete request with the body
//
//	{
//	   "method": "GET",
//	   "hots": "example.com",
//	   "uri": "/static?ext=txt",
//	}
type PurgePayload struct {
	Method string `json:"method"`
	Host   string `json:"host"`
	URI    string `json:"uri"`
	path   string
	query  string
}

func (p *PurgePayload) parseMethod() {
	if p.Method == "" {
		p.Method = "GET" // set GET as default
	}
}

func (p *PurgePayload) parseURI() {
	tokens := strings.Split(p.URI, "?")
	if len(tokens) > 1 {
		p.query = tokens[1]
	}
	p.path = tokens[0]
}

func (p *PurgePayload) pruneHost() {

	if strings.HasPrefix(p.Host, "http") || strings.HasPrefix(p.Host, "https") {
		p.Host = strings.Split(p.Host, "//")[1]
	}

	if !strings.HasSuffix(p.Host, "/") {
		p.Host = p.Host + "/"
	}
}

func (p *PurgePayload) transform() {
	p.parseMethod()
	p.parseURI()
	p.pruneHost()
}

// CaddyModule returns the Caddy module
func (cachePurge) CaddyModule() caddy.ModuleInfo {

	return caddy.ModuleInfo{
		ID:  "admin.api.purge",
		New: func() caddy.Module { return new(cachePurge) },
	}
}

// Purge deletes the cache
func (cachePurge) Purge(cacheHandler *HTTPCache, conds string) error {
	// Regular expression will be a little slow.
	// In fact, there will not be so many keys in real world case
	// so I think this will not be the performance's bottleneck
	keys := cache.Keys()
	r, _ := regexp.Compile(conds)

	for _, k := range keys {
		if r.MatchString(k) {
			if err := cache.Del(k); err != nil {
				return err
			}
		}
	}

	return nil
}

// Routes return a route for the /purge endpoint
func (c cachePurge) Routes() []caddy.AdminRoute {
	return []caddy.AdminRoute{
		{
			Pattern: "/health",
			Handler: caddy.AdminHandlerFunc(health),
		},
		{
			Pattern: "/caches/purge",
			Handler: caddy.AdminHandlerFunc(c.handlePurge),
		},
		{
			Pattern: "/caches/",
			Handler: caddy.AdminHandlerFunc(c.handleCacheEndpoints),
		},
	}
}

func health(w http.ResponseWriter, r *http.Request) error {
	w.WriteHeader(200)
	w.Write([]byte(`OK`))
	return nil
}

func (c cachePurge) handleShowCache(w http.ResponseWriter, r *http.Request) error {
	var err error

	if r.Method != http.MethodGet {
		return caddy.APIError{
			HTTPStatus: http.StatusMethodNotAllowed,
			Err:        fmt.Errorf("method not allowed"),
		}
	}

	key := helper.TrimBy(r.URL.Path, "/", 2)
	cache := getHandlerCache()

	entry, exists := cache.Get(key, r, false)
	if exists {
		err = entry.WriteBodyTo(w)
	}

	return err
}

func (c cachePurge) handleCacheEndpoints(w http.ResponseWriter, r *http.Request) error {
	// a workaround for handling the wildcard endpoint. Caddy uses the standard library's mux
	// so it doesn't support this natively.

	path := r.URL.Path

	switch path {
	case "/caches/":
		return c.handleListCacheKeys(w, r)
	default:
		return c.handleShowCache(w, r)
	}
}

func (c cachePurge) handleListCacheKeys(w http.ResponseWriter, r *http.Request) error {

	if r.Method != http.MethodGet {
		return caddy.APIError{
			HTTPStatus: http.StatusMethodNotAllowed,
			Err:        fmt.Errorf("method not allowed"),
		}
	}

	cache := getHandlerCache()
	keys := cache.Keys()

	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(keys)
	if err != nil {
		return caddy.APIError{
			HTTPStatus: http.StatusBadRequest,
			Err:        err,
		}
	}

	return nil
}

// handlePurge purges the cache matched the provided conditions
func (c cachePurge) handlePurge(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodDelete {
		return caddy.APIError{
			HTTPStatus: http.StatusMethodNotAllowed,
			Err:        fmt.Errorf("method not allowed"),
		}
	}

	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)

	_, err := io.Copy(buf, r.Body)
	if err != nil {
		return caddy.APIError{
			HTTPStatus: http.StatusBadRequest,
			Err:        fmt.Errorf("reading request body: %s", err.Error()),
		}
	}

	// pass the body's content to the Del
	body := buf.Bytes()
	payload := &PurgePayload{}

	err = json.Unmarshal(body, &payload)
	if err != nil {
		return err
	}

	payload.transform()

	purgeRepl.Set("http.request.method", payload.Method)
	purgeRepl.Set("http.request.host", payload.Host)
	purgeRepl.Set("http.request.uri.query", payload.query)
	purgeRepl.Set("http.request.uri.path", payload.path)

	cache := getHandlerCache()
	// example key should be like "GET localhost/static/js/chunk-element.js?"
	i := strings.Index(config.CacheKeyTemplate, "?")
	escapedKeyTmpl := config.CacheKeyTemplate[:i] + "\\" + config.CacheKeyTemplate[i:]

	conds := purgeRepl.ReplaceKnown(escapedKeyTmpl, "")
	return c.Purge(cache, conds)
}
