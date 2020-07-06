package httpcache

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

func makeRequest(url string, headers http.Header) *http.Request {
	r := httptest.NewRequest("GET", url, nil)
	copyHeaders(headers, r.Header)
	return r
}

func makeResponse(code int, headers http.Header) *Response {
	return &Response{
		Code:       code,
		snapHeader: headers,
	}
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

func TestCacheStatusTestSuite(t *testing.T) {
	suite.Run(t, new(CacheStatusTestSuite))
	suite.Run(t, new(RuleMatcherTestSuite))
}
