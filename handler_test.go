package httpcache

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/stretchr/testify/suite"
)

type CacheKeyTemplatingTestSuite struct {
	suite.Suite
	requestURI string
}

func (suite *CacheKeyTemplatingTestSuite) SetupSuite() {
	suite.requestURI = "https://example.com/songa?age=20&class=A"
}

func (suite *CacheKeyTemplatingTestSuite) TestKeyReplacer() {
	r, err := http.NewRequest("GET", suite.requestURI, nil)
	if err != nil {
		suite.Error(err)
	}
	repl := caddyhttp.NewTestReplacer(r)
	result := repl.ReplaceKnown(defaultCacheKeyTemplate, "")

	suite.Equal("GET example.com/songa?age=20&class=A", result)
}

type DetermineShouldCacheTestSuite struct {
	suite.Suite
}

func (suite *DetermineShouldCacheTestSuite) TestWebsocketConnection() {
	eligibleHeader := http.Header{
		"Connection": {"Upgrade"},
		"Upgrade":    {"Websocket"},
	}

	nonEligibleHeader := http.Header{
		"Connection": {"Upgrade"},
		"Upgrade":    {"NoWebsocket"},
	}

	tests := []struct {
		header         http.Header
		shouldBeCached bool
	}{
		{eligibleHeader, false},
		{nonEligibleHeader, true},
	}

	for _, test := range tests {
		req := makeRequest("/", test.header)
		shouldBeCached := shouldUseCache(req)
		suite.Equal(test.shouldBeCached, shouldBeCached)
	}

}

func (suite *DetermineShouldCacheTestSuite) TestNonGETOrHeadMethod() {
	r := httptest.NewRequest("POST", "/", nil)
	shouldBeCached := shouldUseCache(r)
	suite.False(shouldBeCached)
}

func TestCacheKeyTemplatingTestSuite(t *testing.T) {
	suite.Run(t, new(CacheKeyTemplatingTestSuite))
	suite.Run(t, new(DetermineShouldCacheTestSuite))

}
