package httpcache

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2/caddytest"
	"github.com/stretchr/testify/suite"
)

type CacheEndpointTestSuite struct {
	suite.Suite
	caddyTester *caddytest.Tester
	url         string
}

func (suite *CacheEndpointTestSuite) assertKeyNotIn(key string, keys []string, msgAndArgs ...interface{}) {
	exists := false

	for _, k := range keys {
		if k == key {
			exists = true
		}
	}

	suite.False(exists, msgAndArgs)
}

func (suite *CacheEndpointTestSuite) assertKeyIn(key string, keys []string, msgAndArgs ...interface{}) {
	exists := false

	for _, k := range keys {
		if k == key {
			exists = true
		}
	}

	suite.True(exists, msgAndArgs)
}

func (suite *CacheEndpointTestSuite) SetupSuite() {
	suite.caddyTester = caddytest.NewTester(suite.T())
	suite.url = "http://localhost:9898/hello"
	suite.caddyTester.InitServer(`
	{
		order http_cache before reverse_proxy
		admin 0.0.0.0:7777
	}

	:9898 {

		reverse_proxy {
			to localhost:9988
		}

		http_cache {
			cache_type in_memory
		}

	}

	:9988 {
		respond /hello 200 {
			body "hope anything will be good"
		}
	}

	`, "caddyfile")
}

func (suite *CacheEndpointTestSuite) TestListCacheKeys() {
	r, err := http.NewRequest("GET", suite.url, nil)
	suite.Assert().NoError(err)

	res, err := suite.caddyTester.Client.Do(r)
	suite.Assert().NoError(err)
	// create the cache first

	r, err = http.NewRequest("GET", "http://localhost:7777/caches", nil)
	suite.Assert().NoError(err)

	res, err = suite.caddyTester.Client.Do(r)
	suite.Assert().NoError(err)

	result, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	suite.Assert().NoError(err)
	suite.True(strings.Contains(string(result), "GET localhost/hello?"))

}

func (suite *CacheEndpointTestSuite) TestHealthCheck() {
	r, err := http.NewRequest("GET", "http://localhost:7777/health", nil)
	suite.Assert().NoError(err)
	_, err = suite.caddyTester.Client.Do(r)
	suite.Assert().NoError(err)
}

func (suite *CacheEndpointTestSuite) TestShowCache() {
	r, err := http.NewRequest("GET", suite.url, nil)
	suite.Assert().NoError(err)

	_, err = suite.caddyTester.Client.Do(r)
	suite.Assert().NoError(err)
	// create the cache first
	url := fmt.Sprintf("http://localhost:7777/caches/%s", url.PathEscape("GET localhost/hello?"))

	r, err = http.NewRequest("GET", url, nil)
	suite.Assert().NoError(err)

	res, err := suite.caddyTester.Client.Do(r)
	suite.Assert().NoError(err)

	result, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	suite.Assert().NoError(err)
	suite.True(strings.Contains(string(result), "hope anything will be good"), fmt.Sprintf("result: %s", string(result)))
}

func (suite *CacheEndpointTestSuite) TestPurgeCache() {

	testdata := []struct {
		uri      string
		host     string
		body     []byte
		cacheKey string
	}{
		{
			host:     "http://localhost:9898/",
			uri:      "hello",
			body:     []byte(`{"method": "GET", "host": "http://localhost", "uri": "hello"}`),
			cacheKey: "GET localhost/hello?",
		},
		{
			host:     "http://localhost:9898/",
			uri:      "hello?abc.txt",
			body:     []byte(`{"host": "http://localhost", "uri": "hello?abc.txt"}`), // default method is GET
			cacheKey: "GET localhost/hello?abc.txt",
		},
		{
			host:     "http://localhost:9898/",
			uri:      "hello",
			body:     []byte(`{"host": "http://localhost/", "uri": "hello"}`), // host with trailing forward slash is also ok
			cacheKey: "GET localhost/hello?",
		},
	}

	for _, data := range testdata {

		r, err := http.NewRequest("GET", data.host+data.uri, nil)
		suite.Assert().NoError(err)

		_, err = suite.caddyTester.Client.Do(r)
		suite.Assert().NoError(err)
		// create the cache first

		r, err = http.NewRequest("DELETE", "http://localhost:7777/caches/purge", bytes.NewBuffer(data.body))
		suite.Assert().NoError(err)
		r.Header.Set("Content-Type", "application/json")

		cache = getHandlerCache()
		keys := cache.Keys()

		suite.assertKeyIn(data.cacheKey, keys, fmt.Sprintf("%s should be in keys: %s", data.cacheKey, keys))

		res, err := suite.caddyTester.Client.Do(r)
		suite.Assert().NoError(err)
		suite.Equal(200, res.StatusCode)

		// now, the cache is deleted so the key is not existed.
		keys = cache.Keys()
		suite.assertKeyNotIn(data.cacheKey, keys, fmt.Sprintf("%s should not be in keys: %s", data.cacheKey, keys))

	}

}

func TestCacheEndpoingTestSuite(t *testing.T) {
	suite.Run(t, new(CacheEndpointTestSuite))
}
