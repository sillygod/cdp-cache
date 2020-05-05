package httpcache

import (
	"context"
	"encoding/json"

	"net/http"

	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

const (
	cacheHit    = "hit"
	cacheMiss   = "miss"
	cacheSkip   = "skip"
	cacheBypass = "bypass"
)

var (
	contextKeysToPreserve = [...]caddy.CtxKey{
		caddy.ReplacerCtxKey,
		"path_prefix",
		"mitm",
	}
)

func init() {
	caddy.RegisterModule(Handler{})
}

// Handler is a http handler as a middleware to cache the response
type Handler struct {
	Config   *Config     `json:"config,omitempty"`
	Cache    *HTTPCache  `json:"-"`
	URLLocks *URLLock    `json:"-"`
	logger   *zap.Logger `json:"-"`
}

func (h *Handler) addStatusHeaderIfConfigured(w http.ResponseWriter, status string) {
	// TODO: check this part
	// NOTE: in the module header, there are some relavant code.
	// if rec, ok := w.(*httpserver.ResponseRecorder); ok {
	// 	rec.Replacer.Set("cache_status", status)
	// }

	if h.Config.StatusHeader != "" {
		w.Header().Add(h.Config.StatusHeader, status)
	}
}

func (h *Handler) respond(w http.ResponseWriter, entry *Entry, cacheStatus string) error {
	h.addStatusHeaderIfConfigured(w, cacheStatus)
	copyHeaders(entry.Response.snapHeader, w.Header())
	// w.WriteHeader(entry.Response.Code)
	err := entry.WriteBodyTo(w)
	return err
}

func popOrNil(h *Handler, errChan chan error) (err error) {
	// TODO: check this
	select {
	case err := <-errChan:
		h.logger.Error(err.Error())
	default:
	}
	return
}

func (h *Handler) fetchUpstream(req *http.Request, next caddyhttp.Handler) (*Entry, error) {
	// Create a new empty response
	response := NewResponse()

	errChan := make(chan error, 1)

	// Do the upstream fetching in background
	go func(req *http.Request, response *Response) {
		// Create a new context to avoid terminating the Next.ServeHTTP when the original
		// request is closed. Otherwise if the original request is cancelled the other requests
		// will see a bad response that has the same contents the first request has
		updatedContext := context.Background()

		// TODO: find a way to copy the origin request...
		// The problem of cloning the context is that the original one has some values used by
		// other middlewares. If those values are not present they break, #22 is an example.
		// However there isn't a way to know which values a context has. I took the ones that
		// I found on caddy code. If in a future there are new ones this might break.
		// In that case this will have to change to another way
		for _, key := range contextKeysToPreserve {
			value := req.Context().Value(key)
			if value != nil {
				updatedContext = context.WithValue(updatedContext, key, value)
			}
		}

		updatedReq := req.WithContext(updatedContext)

		upstreamError := next.ServeHTTP(response, updatedReq)
		errChan <- upstreamError

		// If status code was not set, this will not replace it
		// It will only ensure status code IS send
		// response.WriteHeader(response.Code) will be called in the ServeHTTP

		// Wait the response body to be set.
		// If it is private it will be the original http.ResponseWriter
		// It is required to wait the body to prevent closing the response
		// before the body was set. If that happens the body will
		// stay locked waiting the response to be closed
		response.WaitBody()
		response.Close()
	}(req, response)

	// Wait headers to be sent
	response.WaitHeaders()

	// Create a new CacheEntry
	return NewEntry(getKey(h.Config.CacheKeyTemplate, req), req, response, h.Config), popOrNil(h, errChan)
}

// CaddyModule returns the Caddy module information
func (Handler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.http_cache",
		New: func() caddy.Module { return new(Handler) },
	}
}

// Provision setups the configs
func (h *Handler) Provision(ctx caddy.Context) error {

	h.logger = ctx.Logger(h)

	if h.Config == nil {
		h.Config = getDefaultConfig()
	}

	for _, raw := range h.Config.RuleMatchersRaws {

		switch raw.Type {
		case MatcherTypePath:

			var content *PathRuleMatcher
			err := json.Unmarshal(raw.Data, &content)
			if err != nil {
				return err
			}

			h.Config.RuleMatchers = append(h.Config.RuleMatchers, content)

		case MatcherTypeHeader:

			var content *HeaderRuleMatcher
			err := json.Unmarshal(raw.Data, &content)
			if err != nil {
				return err
			}

			h.Config.RuleMatchers = append(h.Config.RuleMatchers, content)
		}

	}

	h.Cache = NewHTTPCache(h.Config)
	h.URLLocks = NewURLLock(h.Config)
	return nil
}

// Validate validates httpcache's configuration.
func (h *Handler) Validate() error {

	return nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {

	// add a log here to record the elapsed time (from receiving the request to send the reponse)
	start := time.Now()

	defer func(h *Handler, t time.Time) {
		duration := time.Since(t)
		h.logger.Info("cache handler",
			zap.String("host", r.Host), // find a way to get upstream
			zap.String("method", r.Method),
			zap.String("uri", r.RequestURI),
			zap.Duration("duration", duration))
	}(h, start)

	if !shouldUseCache(r) {
		h.addStatusHeaderIfConfigured(w, cacheBypass)
		return next.ServeHTTP(w, r)
	}

	// TODO: consider to remove this lock
	key := getKey(h.Config.CacheKeyTemplate, r)
	lock := h.URLLocks.Adquire(key)
	defer lock.Unlock()

	// Lookup correct entry
	previousEntry, exists := h.Cache.Get(r)

	// First case: CACHE HIT
	// The response exists in cache and is public
	// It should be served as saved
	if exists && previousEntry.isPublic {
		if err := h.respond(w, previousEntry, cacheHit); err == nil {
			return nil
		}
		exists = false
	}

	// Second case: CACHE SKIP
	// The response is in cache but it is not public
	// It should NOT be served from cache
	// It should be fetched from upstream and check the new headers
	// To check if the new response changes to public
	if exists && !previousEntry.isPublic {
		entry, err := h.fetchUpstream(r, next)
		if err != nil {
			w.WriteHeader(entry.Response.Code)
			return err
		}

		// Case when response was private but now is public
		if entry.isPublic {
			err := entry.setBackend(h.Config)
			if err != nil {
				w.WriteHeader(500)
				return err
			}

			h.Cache.Put(r, entry)
			return h.respond(w, entry, cacheMiss)
		}

		return h.respond(w, entry, cacheSkip)
	}

	// Third case: CACHE MISS
	// The response is not in cache
	// It should be fetched from upstream and save it in cache
	entry, err := h.fetchUpstream(r, next)
	if err != nil {
		w.WriteHeader(entry.Response.Code)
		return err
	}

	// Entry is always saved, even if it is not public
	// This is to release the URL lock.
	if entry.isPublic {
		err := entry.setBackend(h.Config)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return err
		}
	}

	h.Cache.Put(r, entry)
	return h.respond(w, entry, cacheMiss)

}

var (
	_ caddyhttp.MiddlewareHandler = (*Handler)(nil)
	_ caddy.Provisioner           = (*Handler)(nil)
	_ caddy.Validator             = (*Handler)(nil)
)
