package httpcache

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"time"

	"net/http"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/sillygod/cdp-cache/backends"
	"github.com/sillygod/cdp-cache/extends/distributed"
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

	cache  *HTTPCache
	config *Config
)

func init() {
	caddy.RegisterModule(Handler{})
}

// getHandlerCache is a singleton of HTTPCache
func getHandlerCache() *HTTPCache {
	l.RLock()
	defer l.RUnlock()
	return cache
}

// Handler is a http handler as a middleware to cache the response
type Handler struct {
	Config   *Config    `json:"config,omitempty"`
	Cache    *HTTPCache `json:"-"`
	URLLocks *URLLock   `json:"-"`

	DistributedRaw json.RawMessage           `json:"distributed,omitempty" caddy:"namespace=distributed inline_key=distributed"`
	Distributed    distributed.ConsulService `json:"-"`

	logger *zap.Logger
}

func (h *Handler) addStatusHeaderIfConfigured(w http.ResponseWriter, status string) {
	if h.Config.StatusHeader != "" {
		w.Header().Add(h.Config.StatusHeader, status)
	}
}

func (h *Handler) respond(w http.ResponseWriter, entry *Entry, cacheStatus string) error {
	h.addStatusHeaderIfConfigured(w, cacheStatus)
	copyHeaders(entry.Response.snapHeader, w.Header())
	// w.WriteHeader(entry.Response.Code)
	err := entry.WriteBodyTo(w)
	if err != nil {
		h.logger.Error("cache handler", zap.Error(err))
		w.WriteHeader(entry.Response.Code)
		debug.PrintStack()
	}
	return err
}

func popOrNil(h *Handler, errChan chan error) (err error) {
	select {
	case err := <-errChan:
		if err != nil {
			h.logger.Error(fmt.Sprintf("popOrNil: %s", err.Error()))
		}
		return err
	default:
		return nil
	}

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
		defer response.Close()

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

	// NOTE: A dirty work to assign the config and cache to global vars
	// There will be the corresponding functions to get each of them.
	// Therefore, we can call its Del to purge the cache via the admin interface
	cache = NewHTTPCache(h.Config)
	h.Cache = cache
	h.URLLocks = NewURLLock(h.Config)

	// Some type of the backends need extra initialization.
	switch h.Config.Type {
	case inMemory:
		if err := backends.InitGroupCacheRes(h.Config.CacheMaxMemorySize); err != nil {
			return err
		}

	case redis:
		opts, err := backends.ParseRedisConfig(h.Config.RedisConnectionSetting)
		if err != nil {
			return err
		}

		if err := backends.InitRedisClient(opts.Addr, opts.Password, opts.DB); err != nil {
			return err
		}
	}

	// load the guest module distributed
	if h.DistributedRaw != nil {
		val, err := ctx.LoadModule(h, "DistributedRaw") // this will call provision
		if err != nil {
			return fmt.Errorf("loading distributed module: %s", err.Error())
		}
		h.Distributed = *val.(*distributed.ConsulService)

	}

	config = h.Config

	return nil
}

// Validate validates httpcache's configuration.
func (h *Handler) Validate() error {

	return nil
}

func (h *Handler) Cleanup() error {
	// NOTE: release the resources
	return nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	// add a log here to record the elapsed time (from receiving the request to send the response)
	start := time.Now()
	upstreamDuration := time.Duration(0)

	// TODO: think a proper way to log these info
	// research why can not write the log into files.
	defer func(h *Handler, t time.Time) {

		if upstreamDuration == 0 {
			duration := time.Since(t)
			h.logger.Debug("cache handler",
				zap.String("host", r.Host), // find a way to get upstream
				zap.String("method", r.Method),
				zap.String("uri", r.RequestURI),
				zap.Duration("request time", duration))
		} else {
			duration := time.Since(t)
			h.logger.Debug("cache handler",
				zap.String("host", r.Host), // find a way to get upstream
				zap.String("method", r.Method),
				zap.String("uri", r.RequestURI),
				zap.Duration("request time", duration),
				zap.Duration("upstream request time", upstreamDuration))
		}

	}(h, start)

	if !shouldUseCache(r) {
		h.addStatusHeaderIfConfigured(w, cacheBypass)
		return next.ServeHTTP(w, r)
	}

	key := getKey(h.Config.CacheKeyTemplate, r)
	lock := h.URLLocks.Acquire(key)
	defer lock.Unlock()

	// Lookup correct entry
	previousEntry, exists := h.Cache.Get(key, r)

	// First case: CACHE HIT
	// The response exists in cache and is public
	// It should be served as saved
	if exists && previousEntry.isPublic {
		if err := h.respond(w, previousEntry, cacheHit); err == nil {
			return nil
		}
	}

	// Second case: CACHE SKIP
	// The response is in cache but it is not public
	// It should NOT be served from cache
	// It should be fetched from upstream and check the new headers
	// To check if the new response changes to public

	// Third case: CACHE MISS
	// The response is not in cache
	// It should be fetched from upstream and save it in cache

	t := time.Now()
	entry, err := h.fetchUpstream(r, next)
	upstreamDuration = time.Since(t)

	if err != nil {
		w.WriteHeader(entry.Response.Code)
		return err
	}

	// Case when response was private but now is public
	if entry.isPublic {
		err := entry.setBackend(r.Context(), h.Config)
		if err != nil {
			w.WriteHeader(500)
			return err
		}

		h.Cache.Put(r, entry)
		return h.respond(w, entry, cacheMiss)
	}

	return h.respond(w, entry, cacheSkip)

}

var (
	_ caddyhttp.MiddlewareHandler = (*Handler)(nil)
	_ caddy.Provisioner           = (*Handler)(nil)
	_ caddy.Validator             = (*Handler)(nil)
)
