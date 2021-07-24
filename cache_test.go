package httpcache

import (
	"bytes"
	"context"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/sillygod/cdp-cache/backends"
	"github.com/stretchr/testify/suite"
)

func makeRequest(url string, headers http.Header) *http.Request {
	r := httptest.NewRequest("GET", url, nil)
	copyHeaders(headers, r.Header)
	return r
}

func makeResponse(code int, headers http.Header) *Response {
	response := NewResponse()
	response.Code = code
	response.snapHeader = headers
	return response
}

func makeHeader(key string, value string) http.Header {
	h := http.Header{}
	h.Add(key, value)
	return h
}

type CacheStatusTestSuite struct {
	suite.Suite
	c *Config
}

func (suite *CacheStatusTestSuite) SetupSuite() {
	suite.c = &Config{}
	suite.c.DefaultMaxAge = 1 * time.Second
	suite.c.LockTimeout = 5 * time.Hour
	suite.c.RuleMatchers = []RuleMatcher{
		&PathRuleMatcher{Path: "/public"},
	}

	testTime := time.Now().UTC()
	// monkey patch the origin definition of now
	now = func() time.Time {
		return testTime
	}
}

func (suite *CacheStatusTestSuite) TearDownSuite() {
	now = time.Now().UTC
}

func (suite *CacheStatusTestSuite) TestCacheControlParseError() {
	// cache-control: https://www.imperva.com/learn/performance/cache-control/#:~:text=Cache%2DControl%3A%20Max%2DAge,another%20request%20to%20a%20server.
	req := makeRequest("/", http.Header{})
	res := makeResponse(200, makeHeader("Cache-Control", "max-age=song"))
	isPublic, expiration := getCacheStatus(req, res, suite.c)
	suite.False(isPublic)
	suite.Equal(time.Time{}, expiration)
}

func (suite *CacheStatusTestSuite) TestCacheControlIsPrivate() {
	req := makeRequest("/", http.Header{})
	res := makeResponse(200, makeHeader("Cache-Control", "private"))
	isPublic, expiration := getCacheStatus(req, res, suite.c)
	suite.False(isPublic)
	suite.Equal(now().Add(suite.c.LockTimeout), expiration, "lockTimeout should be returned")
}

func (suite *CacheStatusTestSuite) TestVaryWildCardInResponseHeader() {
	// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Vary
	req := makeRequest("/", http.Header{})
	res := makeResponse(200, makeHeader("Vary", "*"))
	isPublic, expiration := getCacheStatus(req, res, suite.c)
	suite.False(isPublic)
	suite.Equal(now().Add(suite.c.LockTimeout), expiration)
}

func (suite *CacheStatusTestSuite) TestUpstreamReturned502() {
	req := makeRequest("/", http.Header{})
	res := makeResponse(502, http.Header{})
	isPublic, _ := getCacheStatus(req, res, suite.c)
	suite.False(isPublic)
}

func (suite *CacheStatusTestSuite) TestUpstreamReturned304() {
	req := makeRequest("/", http.Header{})
	res := makeResponse(304, http.Header{})
	isPublic, _ := getCacheStatus(req, res, suite.c)
	suite.False(isPublic)
}

func (suite *CacheStatusTestSuite) TestPathMatchedWithExpirationSpecified() {
	req := makeRequest("/public", http.Header{})
	res := makeResponse(200, makeHeader("Cache-control", "max-age=5"))
	isPublic, expiration := getCacheStatus(req, res, suite.c)
	suite.True(isPublic)
	suite.Equal(now().Add(time.Duration(5)*time.Second).Round(time.Second), expiration.Round(time.Second))
}

func (suite *CacheStatusTestSuite) TestPathMatchedWithoutExpirationSpecified() {
	req := makeRequest("/public", http.Header{})
	res := makeResponse(200, http.Header{})
	isPublic, expiration := getCacheStatus(req, res, suite.c)
	suite.True(isPublic)
	suite.Equal(now().Add(suite.c.DefaultMaxAge).Round(time.Second), expiration.Round(time.Second))
}

type RuleMatcherTestSuite struct {
	suite.Suite
}

func (suite *RuleMatcherTestSuite) TestPathMatched() {
	m := PathRuleMatcher{Path: "/"}
	match := m.matches(makeRequest("/", http.Header{}), 200, http.Header{})
	suite.True(match)
}

func (suite *RuleMatcherTestSuite) TestPathNotMatched() {
	m := PathRuleMatcher{Path: "/is"}
	match := m.matches(makeRequest("/", http.Header{}), 200, http.Header{})
	suite.False(match)
}

func (suite *RuleMatcherTestSuite) TestHeaderMatched() {
	m := HeaderRuleMatcher{
		Header: "Content-Type",
		Value:  []string{"image/png", "image/jpg"}}

	match := m.matches(makeRequest("/", http.Header{}), 200, makeHeader("Content-Type", "image/jpg"))
	suite.True(match)
}

func (suite *RuleMatcherTestSuite) TestHeaderNotMatched() {
	m := HeaderRuleMatcher{
		Header: "Content-Type",
		Value:  []string{"image/png", "image/jpg"}}

	match := m.matches(makeRequest("/", http.Header{}), 200, makeHeader("Content-Type", "application/json"))
	suite.False(match)
}

type EntryTestSuite struct {
	suite.Suite
	config *Config
}

func (suite *EntryTestSuite) SetupSuite() {
	err := backends.InitGroupCacheRes(50 * 1024 * 1024)
	suite.Nil(err)
	suite.config = getDefaultConfig()
}

func (suite *EntryTestSuite) TestEntryWritePublicResponse() {

	req := makeRequest("/", http.Header{})
	res := makeResponse(200, makeHeader("Cache-Control", "max-age=43200"))
	entry := NewEntry("unique_key", req, res, suite.config)

	suite.Equal("unique_key", entry.Key())
	rw := httptest.NewRecorder()

	suite.config.Type = inMemory
	input := []byte(`rain cats and dogs`)

	go func() {
		entry.Response.Write(input)
		entry.Response.Close() // we can write the entry's body to rw after closing upstream response
	}()

	entry.setBackend(req.Context(), suite.config)

	entry.WriteBodyTo(rw)
	result, err := ioutil.ReadAll(rw.Result().Body)
	suite.Nil(err)
	suite.Equal(input, result)
}

func (suite *EntryTestSuite) TestEntryWritePrivateResponse() {
	req := makeRequest("/", http.Header{})
	res := makeResponse(502, http.Header{})
	entry := NewEntry("unique_key2", req, res, suite.config)

	go func() {
		entry.Response.Write([]byte(`Bad Gateway`))
		entry.Response.Close()
	}()

	rw := httptest.NewRecorder()
	entry.WriteBodyTo(rw)
	suite.Equal(502, rw.Code)
}

func (suite *EntryTestSuite) TearDownSuite() {
	err := backends.ReleaseGroupCacheRes()
	suite.Nil(err)
}

type HTTPCacheTestSuite struct {
	suite.Suite
	config *Config
	cache  *HTTPCache
}

func (suite *HTTPCacheTestSuite) SetupSuite() {
	err := backends.InitGroupCacheRes(50 * 1024 * 1024)
	suite.Nil(err)
	suite.config = getDefaultConfig()
	suite.cache = NewHTTPCache(suite.config, false)
}

func (suite *HTTPCacheTestSuite) TestGetNonExistEntry() {
	req := makeRequest("/", http.Header{})
	entry, exists := suite.cache.Get("abc", req)
	suite.Nil(entry)
	suite.False(exists)
}

func (suite *HTTPCacheTestSuite) TestKeyWithrespectVary() {
	req := makeRequest("/", http.Header{
		"accept-encoding": []string{"gzip, deflate, br"},
	})
	res := makeResponse(200, http.Header{
		"Vary": []string{"Accept-Encoding"},
	})
	e := NewEntry("hello", req, res, suite.config)
	key := e.keyWithRespectVary()
	expected := "hello" + url.PathEscape("gzip, deflate, br")
	suite.Equal(expected, key)
}

func (suite *HTTPCacheTestSuite) TestGetExistEntry() {
	req := makeRequest("/", http.Header{})
	res := makeResponse(200, http.Header{})
	entry := NewEntry("hello", req, res, suite.config)
	suite.cache.Put(req, entry)

	prevEntry, exists := suite.cache.Get("hello", req)
	suite.Equal(prevEntry, entry)
	suite.True(exists)
}

func (suite *HTTPCacheTestSuite) TestCleanEntry() {
	req := makeRequest("/", http.Header{})
	res := makeResponse(200, http.Header{})
	key := "friday"

	entry := NewEntry(key, req, res, suite.config)
	suite.cache.Put(req, entry)

	keyInKeys := false
	keys := suite.cache.Keys()
	for _, k := range keys {
		if k == key {
			keyInKeys = true
		}
	}
	suite.True(keyInKeys)

	err := suite.cache.Del(key)
	suite.Nil(err)
}

func (suite *HTTPCacheTestSuite) TearDownSuite() {
	err := backends.ReleaseGroupCacheRes()
	suite.Nil(err)
}

type KeyTestSuite struct {
	suite.Suite
}

func (suite *KeyTestSuite) TestContentLengthInKey() {
	body := []byte(`{"search":"my search string"}`)
	req := httptest.NewRequest("POST", "/", bytes.NewBuffer(body))
	ctx := context.WithValue(req.Context(), caddy.ReplacerCtxKey, caddyhttp.NewTestReplacer(req))
	req = req.WithContext(ctx)
	key := getKey("{http.request.contentlength}", req)
	suite.Equal("29", key)
}

func (suite *KeyTestSuite) TestBodyHashInKey() {
	body := []byte(`{"search":"my search string"}`)
	req := httptest.NewRequest("POST", "/", bytes.NewBuffer(body))
	ctx := context.WithValue(req.Context(), caddy.ReplacerCtxKey, caddyhttp.NewTestReplacer(req))
	req = req.WithContext(ctx)
	key := getKey("{http.request.bodyhash}", req)
	suite.Equal("5edeb27ddae03685d04df2ab56ebf11fb9c8a711", key)
}

func TestCacheStatusTestSuite(t *testing.T) {
	suite.Run(t, new(CacheStatusTestSuite))
	suite.Run(t, new(HTTPCacheTestSuite))
	suite.Run(t, new(RuleMatcherTestSuite))
	suite.Run(t, new(EntryTestSuite))
	suite.Run(t, new(KeyTestSuite))
}
