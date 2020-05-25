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

var (
	entries     []map[string][]*Entry
	entriesLock []*sync.RWMutex
	l           sync.RWMutex
)

// RuleMatcherType specifies the type of matching rule to cache.
type RuleMatcherType string

// the following list the different way to decide the request
// whether is matched or not to be cached it's response.
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

func expirationObject(obj *cacheobject.Object, rv *cacheobject.ObjectResults) {
	/**
	 * Okay, lets calculate Freshness/Expiration now. woo:
	 *  http://tools.ietf.org/html/rfc7234#section-4.2
	 */

	/*
	   o  If the cache is shared and the s-maxage response directive
	      (Section 5.2.2.9) is present, use its value, or

	   o  If the max-age response directive (Section 5.2.2.8) is present,
	      use its value, or

	   o  If the Expires response header field (Section 5.3) is present, use
	      its value minus the value of the Date response header field, or

	   o  Otherwise, no explicit expiration time is present in the response.
	      A heuristic freshness lifetime might be applicable; see
	      Section 4.2.2.
	*/

	var expiresTime time.Time

	if obj.RespDirectives.SMaxAge != -1 && !obj.CacheIsPrivate {
		expiresTime = obj.NowUTC.Add(time.Second * time.Duration(obj.RespDirectives.SMaxAge))
	} else if obj.RespDirectives.MaxAge != -1 {
		expiresTime = obj.NowUTC.UTC().Add(time.Second * time.Duration(obj.RespDirectives.MaxAge))
	} else if !obj.RespExpiresHeader.IsZero() {
		serverDate := obj.RespDateHeader
		if serverDate.IsZero() {
			// common enough case when a Date: header has not yet been added to an
			// active response.
			serverDate = obj.NowUTC
		}
		expiresTime = obj.NowUTC.Add(obj.RespExpiresHeader.Sub(serverDate))
	} else {
		expiresTime = obj.NowUTC
	}

	rv.OutExpirationTime = expiresTime
}

func judgeResponseShouldCacheOrNot(req *http.Request,
	statusCode int,
	respHeaders http.Header,
	privateCache bool) ([]cacheobject.Reason, time.Time, []cacheobject.Warning, *cacheobject.Object, error) {

	var reqHeaders http.Header
	var reqMethod string
	var reqDir *cacheobject.RequestCacheDirectives

	respDir, err := cacheobject.ParseResponseCacheControl(respHeaders.Get("Cache-Control"))
	if err != nil {
		return nil, time.Time{}, nil, nil, err
	}

	if req != nil {
		reqDir, err = cacheobject.ParseRequestCacheControl(req.Header.Get("Cache-Control"))
		if err != nil {
			return nil, time.Time{}, nil, nil, err
		}
		reqHeaders = req.Header
		reqMethod = req.Method
	}

	var expiresHeader time.Time
	var dateHeader time.Time
	var lastModifiedHeader time.Time

	if respHeaders.Get("Expires") != "" {
		expiresHeader, err = http.ParseTime(respHeaders.Get("Expires"))
		if err != nil {
			// sometimes servers will return `Expires: 0` or `Expires: -1` to
			// indicate expired content
			expiresHeader = time.Time{}
		}
		expiresHeader = expiresHeader.UTC()
	}

	if respHeaders.Get("Date") != "" {
		dateHeader, err = http.ParseTime(respHeaders.Get("Date"))
		if err != nil {
			return nil, time.Time{}, nil, nil, err
		}
		dateHeader = dateHeader.UTC()
	}

	if respHeaders.Get("Last-Modified") != "" {
		lastModifiedHeader, err = http.ParseTime(respHeaders.Get("Last-Modified"))
		if err != nil {
			return nil, time.Time{}, nil, nil, err
		}
		lastModifiedHeader = lastModifiedHeader.UTC()
	}

	obj := cacheobject.Object{
		CacheIsPrivate: privateCache,

		RespDirectives:         respDir,
		RespHeaders:            respHeaders,
		RespStatusCode:         statusCode,
		RespExpiresHeader:      expiresHeader,
		RespDateHeader:         dateHeader,
		RespLastModifiedHeader: lastModifiedHeader,

		ReqDirectives: reqDir,
		ReqHeaders:    reqHeaders,
		ReqMethod:     reqMethod,

		NowUTC: time.Now().UTC(),
	}
	rv := cacheobject.ObjectResults{}

	cacheobject.CachableObject(&obj, &rv)
	if rv.OutErr != nil {
		return nil, time.Time{}, nil, nil, rv.OutErr
	}

	expirationObject(&obj, &rv)
	if rv.OutErr != nil {
		return nil, time.Time{}, nil, nil, rv.OutErr
	}

	return rv.OutReasons, rv.OutExpirationTime, rv.OutWarnings, &obj, nil
}

func getCacheStatus(req *http.Request, response *Response, config *Config) (bool, time.Time) {
	// TODO: what does the lock time do, add more rule
	if response.Code == http.StatusPartialContent || response.snapHeader.Get("Content-Range") != "" {
		return false, time.Now().Add(config.LockTimeout)
	}

	if response.Code == http.StatusNotModified {
		return false, time.Now()
	}

	reasonsNotToCache, expiration, _, _, err := judgeResponseShouldCacheOrNot(req, response.Code, response.snapHeader, false)
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

// Key return the key for the entry
func (e *Entry) Key() string {
	return e.key
}

// Clean purges the cache
func (e *Entry) Clean() error {
	return e.Response.Clean()
}

func (e *Entry) writePublicResponse(w http.ResponseWriter) error {
	// TODO: Maybe we can redesign here to get a better performance
	reader, err := e.Response.GetReader()

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

// IsFresh indicates this entry is not expired
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
		backend, err = backends.NewInMemoryBackend(ctx, e.key, e.expiration)
	case redis:
		backend, err = backends.NewRedisBackend(ctx, e.key, e.expiration)
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
	// TODO: how to handle when the bucket's num is changed
	l.Lock()
	defer l.Unlock()

	if entries == nil {
		entries = make([]map[string][]*Entry, config.CacheBucketsNum)
		for i := 0; i < config.CacheBucketsNum; i++ {
			entries[i] = make(map[string][]*Entry)
		}
	}

	if entriesLock == nil {
		entriesLock = make([]*sync.RWMutex, config.CacheBucketsNum)
		for i := 0; i < config.CacheBucketsNum; i++ {
			entriesLock[i] = new(sync.RWMutex)
		}
	}

	return &HTTPCache{
		cacheKeyTemplate: config.CacheKeyTemplate,
		cacheBucketsNum:  config.CacheBucketsNum,
		entries:          entries,
		entriesLock:      entriesLock,
	}

}

func (h *HTTPCache) getBucketIndexForKey(key string) uint32 {
	return uint32(math.Mod(float64(crc32.ChecksumIEEE([]byte(key))), float64(h.cacheBucketsNum)))
}

// In caddy2, it is automatically add the map by addHTTPVarsToReplacer
func getKey(cacheKeyTemplate string, r *http.Request) string {
	repl := r.Context().Value(caddy.ReplacerCtxKey).(*caddy.Replacer)
	return repl.ReplaceKnown(cacheKeyTemplate, "")
}

// Get returns the cached response
func (h *HTTPCache) Get(key string, request *http.Request) (*Entry, bool) {
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

// Keys list the keys holded by this cache
func (h *HTTPCache) Keys() []string {
	keys := []string{}
	for index, l := range h.entriesLock {
		l.RLock()
		for k, v := range h.entries[index] {
			if len(v) != 0 {
				keys = append(keys, k)
			}
		}
		l.RUnlock()
	}

	return keys
}

// Del purge the key immediately
func (h *HTTPCache) Del(key string) error {
	b := h.getBucketIndexForKey(key)
	h.entriesLock[b].RLock()
	previousEntries, exists := h.entries[b][key]
	h.entriesLock[b].RUnlock()

	if !exists {
		return nil
	}

	// the schedule will clean the entry automatically
	for _, entry := range previousEntries {
		if entry.IsFresh() {
			err := h.cleanEntry(entry)
			if err != nil {
				caddy.Log().Named("http.handlers.http_cache").Error(fmt.Sprintf("clean entry error: %s", err.Error()))
				return err
			}
		}
	}

	return nil
}

// Put adds the entry in the cache
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

func (h *HTTPCache) cleanEntry(entry *Entry) error {
	key := entry.Key()
	bucket := h.getBucketIndexForKey(key)

	h.entriesLock[bucket].Lock()
	defer h.entriesLock[bucket].Unlock()

	for i, otherEntry := range h.entries[bucket][key] {
		if entry == otherEntry {
			h.entries[bucket][key] = append(h.entries[bucket][key][:i], h.entries[bucket][key][i+1:]...)
			return entry.Clean()
		}
	}

	return nil
}

func (h *HTTPCache) scheduleCleanEntry(entry *Entry) {
	go func(entry *Entry) {
		time.Sleep(entry.expiration.Sub(time.Now()))
		h.cleanEntry(entry)
	}(entry)
}
