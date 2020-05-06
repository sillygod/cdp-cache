package httpcache

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/pquerna/cachecontrol/cacheobject"
	"github.com/sillygod/cdp-cache/backends"
)

// Module lifecycle
// 1. Loaded (the Unmarshaler?)
// 2. Provisioned and validated
// 3. used
// 4. cleaned up

type RuleMatcherType string

const (
	MatcherTypePath   RuleMatcherType = "path"
	MatcherTypeHeader RuleMatcherType = "header"
)

type RuleMatcherRawWithType struct {
	Type RuleMatcherType
	Data json.RawMessage
}

// RuleMatcher determines whether the request should be cached or not.
type RuleMatcher interface {
	matches(*http.Request, int, http.Header) bool
}

// PathRuleMatcher determines whether the reuqest's path is matched.
type PathRuleMatcher struct {
	Path string `json:"path"`
}

func (p *PathRuleMatcher) matches(req *http.Request, statusCode int, resHeaders http.Header) bool {
	return strings.HasPrefix(req.URL.Path, p.Path)
}

// HeaderRuleMatcher determines whether the request's header is matched.
type HeaderRuleMatcher struct {
	Header string   `json:"header"`
	Value  []string `json:"value"`
}

func (h *HeaderRuleMatcher) matches(req *http.Request, statusCode int, resHeaders http.Header) bool {
	headerValues := resHeaders.Get(h.Header)
	for _, value := range h.Value {
		if value == headerValues {
			return true
		}
	}

	return false
}

func getCacheStatus(req *http.Request, response *Response, config *Config) (bool, time.Time) {
	// TODO: what does the lock time do, add more rule
	if response.Code == http.StatusPartialContent || response.snapHeader.Get("Content-Range") != "" {
		return false, time.Now().Add(config.LockTimeout)
	}

	if response.Code == http.StatusNotModified {
		return false, time.Now()
	}

	// TODO: the expiration here.. is weird
	// implement my own cache expire rule
	reasonsNotToCache, expiration, err := cacheobject.UsingRequestResponse(req, response.Code, response.snapHeader, false)
	if err != nil {
		return false, time.Time{}
	}

	isPublic := len(reasonsNotToCache) == 0
	if !isPublic {
		return false, time.Now().Add(config.LockTimeout)
	}

	varyHeader := response.HeaderMap.Get("Vary")
	if varyHeader == "*" {
		return false, time.Now().Add(config.LockTimeout)
	}

	for _, rule := range config.RuleMatchers {
		if rule.matches(req, response.Code, response.snapHeader) {

			if expiration.Before(time.Now()) {
				expiration = time.Now().Add(config.DefaultMaxAge)
			}

			return true, expiration
		}
	}

	return false, time.Now()
}

func matchVary(curReq *http.Request, entry *Entry) bool {
	// NOTE: https://httpwg.org/specs/rfc7231.html#header.vary
	vary := entry.Response.HeaderMap.Get("Vary")

	for _, searchedHeader := range strings.Split(vary, ",") {
		searchedHeader = strings.TrimSpace(searchedHeader)
		if curReq.Header.Get(searchedHeader) != entry.Request.Header.Get(searchedHeader) {
			return false
		}
	}

	return true
}

// Entry consists of a cache key and one or more response corresponding to
// the prior requests.
// https://httpwg.org/specs/rfc7234.html#caching.overview
type Entry struct {
	isPublic   bool
	expiration time.Time
	key        string
	Request    *http.Request
	Response   *Response
}

// NewEntry creates a new Entry for the given request and response
// and it also calculates whether it is public or not
func NewEntry(key string, request *http.Request, response *Response, config *Config) *Entry {
	isPublic, expiration := getCacheStatus(request, response, config)

	fmt.Printf("the expiration time: %s, now is: %s \n", expiration, time.Now())

	return &Entry{
		isPublic:   isPublic,
		key:        key,
		expiration: expiration,
		Request:    request,
		Response:   response,
	}
}

func (e *Entry) Key() string {
	return e.key
}

func (e *Entry) Clean() error {
	return e.Response.Clean()
}

func (e *Entry) writePublicResponse(w http.ResponseWriter) error {
	reader, err := e.Response.body.GetReader()

	if err != nil {
		return err
	}

	defer reader.Close()

	_, err = io.Copy(w, reader)
	return err
}

func (e *Entry) writePrivateResponse(w http.ResponseWriter) error {
	e.Response.SetBody(backends.WrapResponseWriterToBackend(w))
	e.Response.WaitClose()
	return nil
}

// WriteBodyTo sends the body to the http.ResponseWritter
func (e *Entry) WriteBodyTo(w http.ResponseWriter) error {
	// the definition of private response seems come from
	// the package cacheobject
	if !e.isPublic {
		return e.writePrivateResponse(w)
	}

	return e.writePublicResponse(w)
}

func (e *Entry) IsFresh() bool {
	return e.expiration.After(time.Now())
}

func (e *Entry) setBackend(ctx context.Context, config *Config) error {
	var backend backends.Backend
	var err error
	// I can give the context here?
	switch config.Type {
	case file:
		backend, err = backends.NewFileBackend(config.Path)
	case inMemory:
		backend, err = backends.NewInMemoryBackend(ctx, config.CacheKeyTemplate)
	}

	e.Response.SetBody(backend)
	return err
}

// HTTPCache is a http cache for http request which is focus on static files
type HTTPCache struct {
	cacheKeyTemplate string
	cacheBucketsNum  int
	entries          []map[string][]*Entry
	entriesLock      []*sync.RWMutex
}

// NewHTTPCache new a HTTPCache to hanle cache entries
func NewHTTPCache(config *Config) *HTTPCache {
	entries := make([]map[string][]*Entry, config.CacheBucketsNum)
	entriesLock := make([]*sync.RWMutex, config.CacheBucketsNum)

	for i := 0; i < config.CacheBucketsNum; i++ {
		entriesLock[i] = new(sync.RWMutex)
		entries[i] = make(map[string][]*Entry)
	}

	return &HTTPCache{
		cacheKeyTemplate: config.CacheKeyTemplate,
		cacheBucketsNum:  config.CacheBucketsNum,
		entries:          entries,
		entriesLock:      entriesLock,
	}

}

// this will conflict so I think that's why the corresponding
// value is an array. Maybe, there is a more efficient way.
func (h *HTTPCache) getBucketIndexForKey(key string) uint32 {
	return uint32(math.Mod(float64(crc32.ChecksumIEEE([]byte(key))), float64(h.cacheBucketsNum)))
}

// In caddy2, it is automatically add the map by addHTTPVarsToReplacer
func getKey(cacheKeyTemplate string, r *http.Request) string {
	repl := r.Context().Value(caddy.ReplacerCtxKey).(*caddy.Replacer)
	return repl.ReplaceKnown(cacheKeyTemplate, "")
}

// Get returns the cached response
func (h *HTTPCache) Get(request *http.Request) (*Entry, bool) {
	key := getKey(h.cacheKeyTemplate, request)
	b := h.getBucketIndexForKey(key)
	h.entriesLock[b].RLock()
	defer h.entriesLock[b].RUnlock()

	previousEntries, exists := h.entries[b][key]

	if !exists {
		return nil, false
	}

	for _, entry := range previousEntries {
		if entry.IsFresh() && matchVary(request, entry) {
			return entry, true
		}
	}

	return nil, false
}

func (h *HTTPCache) Put(request *http.Request, entry *Entry) {
	key := entry.Key()
	bucket := h.getBucketIndexForKey(key)

	h.entriesLock[bucket].Lock()
	defer h.entriesLock[bucket].Unlock()

	h.scheduleCleanEntry(entry)

	for i, previousEntry := range h.entries[bucket][key] {
		if matchVary(entry.Request, previousEntry) {
			go previousEntry.Clean()
			h.entries[bucket][key][i] = entry
			return
		}
	}

	h.entries[bucket][key] = append(h.entries[bucket][key], entry)
}

func (h *HTTPCache) cleanEntry(entry *Entry) {
	key := entry.Key()
	bucket := h.getBucketIndexForKey(key)

	h.entriesLock[bucket].Lock()
	defer h.entriesLock[bucket].Unlock()

	for i, otherEntry := range h.entries[bucket][key] {
		if entry == otherEntry {
			h.entries[bucket][key] = append(h.entries[bucket][key][:i], h.entries[bucket][key][i+1:]...)
			entry.Clean()
			return
		}
	}
}

func (h *HTTPCache) scheduleCleanEntry(entry *Entry) {
	go func(entry *Entry) {
		time.Sleep(entry.expiration.Sub(time.Now()))
		h.cleanEntry(entry)
	}(entry)
}
