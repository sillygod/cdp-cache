package httpcache

import (
	"context"
	"encoding/json"
	"fmt"
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

	DistributedRaw json.RawMessage            `json:"distributed,omitempty" caddy:"namespace=distributed inline_key=distributed"`
	Distributed    *distributed.ConsulService `json:"-"`

	logger *zap.Logger
}

func (h *Handler) addStatusHeaderIfConfigured(w http.ResponseWriter, status string) {
	if h.Config.StatusHeader != "" {
		w.Header().Set(h.Config.StatusHeader, status)
	}
}

func (h *Handler) respond(w http.ResponseWriter, entry *Entry, cacheStatus string) error {
	h.addStatusHeaderIfConfigured(w, cacheStatus)
	copyHeaders(entry.Response.snapHeader, w.Header())

	// when the request method is head, we don't need ot perform write body
	if entry.Request.Method == "HEAD" {
		w.WriteHeader(entry.Response.Code)
		return nil
	}

	err := entry.WriteBodyTo(w)
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

func (h *Handler) provisionRuleMatchers() error {

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

	return nil
}

func (h *Handler) provisionDistributed(ctx caddy.Context) error {
	if h.DistributedRaw != nil {
		val, err := ctx.LoadModule(h, "DistributedRaw") // this will call provision
		if err != nil {
			return fmt.Errorf("loading distributed module: %s", err.Error())
		}
		h.Distributed = val.(*distributed.ConsulService)
	}

	return nil
}

// Provision setups the configs
func (h *Handler) Provision(ctx caddy.Context) error {

	h.logger = ctx.Logger(h)

	if h.Config == nil {
		h.Config = getDefaultConfig()
	}

	err := h.provisionRuleMatchers()
	if err != nil {
		return err
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
	err = h.provisionDistributed(ctx)
	if err != nil {
		return err
	}

	config = h.Config
	return nil
}

// Validate validates httpcache's configuration.
func (h *Handler) Validate() error {
	return nil
}

// Cleanup release the resources
func (h *Handler) Cleanup() error {
	var err error

	if h.Config.Type == inMemory {
		err = backends.ReleaseGroupCacheRes()
	}

	return err
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
		} else if _, ok := err.(backends.NoPreCollectError); ok {
			// if the err is No pre collect, just return nil
			w.WriteHeader(previousEntry.Response.Code)
			return nil
		}
	}

	// Check whether the key exists in the groupcahce when the
	// distributed cache is enabled.
	// Currently, only support the memory backends
	if h.Distributed != nil {
		// new an entry without fetching the upstream
		response := NewResponse()
		entry := NewEntry(getKey(h.Config.CacheKeyTemplate, r), r, response, h.Config)
		err := entry.setBackend(r.Context(), h.Config)
		if err != nil {
			return caddyhttp.Error(http.StatusInternalServerError, err)
		}

		h.Cache.Put(r, entry)
		response.Close()

		// NOTE: should set the content-length to the header manually when distributed
		// cache is enabled because we get the content from the other peer.
		// In this case, the snapHeader will not contain the content-length info
		if err = h.respond(w, entry, cacheHit); err == nil {
			return nil
		}

		// when error is NoPreCollectError, we need to fetch the resource from the
		// upstream.
		if e, ok := err.(backends.NoPreCollectError); !ok {
			return caddyhttp.Error(entry.Response.Code, e)
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
		return caddyhttp.Error(entry.Response.Code, err)
	}

	// Case when response was private but now is public
	if entry.isPublic {
		err := entry.setBackend(r.Context(), h.Config)
		if err != nil {
			return caddyhttp.Error(http.StatusInternalServerError, err)
		}

		h.Cache.Put(r, entry)
		err = h.respond(w, entry, cacheMiss)
		if err != nil {
			h.logger.Error("cache handler", zap.Error(err))
			return caddyhttp.Error(entry.Response.Code, err)
		}

		return nil
	}

	err = h.respond(w, entry, cacheSkip)
	if err != nil {
		h.logger.Error("cache handler", zap.Error(err))
		return caddyhttp.Error(entry.Response.Code, err)
	}

	return nil
}

var (
	_ caddyhttp.MiddlewareHandler = (*Handler)(nil)
	_ caddy.Provisioner           = (*Handler)(nil)
	_ caddy.Validator             = (*Handler)(nil)
)
