package httpcache

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caddyserver/caddy/v2"
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

type HandlerProvisionTestSuite struct {
	suite.Suite
	handler *Handler
}

func (suite *HandlerProvisionTestSuite) SetupSuite() {
	suite.handler = new(Handler)
	if suite.handler.Config == nil {
		suite.handler.Config = getDefaultConfig()
	}

	suite.handler.Config.RuleMatchersRaws = []RuleMatcherRawWithType{
		{
			Type: MatcherTypePath,
			Data: []byte(`{"path": "/"}`),
		},
		{
			Type: MatcherTypeHeader,
			Data: []byte(`{"header": "Content-Type", "value": ["image/jpg"]}`),
		},
	}
}

func (suite *HandlerProvisionTestSuite) TearDownSuite() {
}

func (suite *HandlerProvisionTestSuite) TestProvisionRuleMatchers() {
	err := suite.handler.provisionRuleMatchers()
	suite.Assert().NoError(err)
}

func (suite *HandlerProvisionTestSuite) TestProvisionDistributed() {

	suite.handler.DistributedRaw = []byte(`
	{
		"Catalog":null,
		"Client":null,
		"Config":{
			"addr":"consul:8500",
			"health_url":":7777/health",
			"service_name":"cache_server"
		},
		"KV":null,
		"ServiceIDs":null,
		"distributed":"consul"}
	`)

	err := suite.handler.provisionDistributed(caddy.Context{})
	// NOTE: I know it's weird here but I want to ensure the part of function LoadModule
	// works correctly. In this case, I don't set up the consul server so it will get
	// connection error like "xxx no such host"
	suite.Contains(err.Error(), "http://consul:8500/v1/agent/service/register")
}

func (suite *HandlerProvisionTestSuite) TestProvisionRedisBackend() {
	suite.handler.Config.Type = redis
	suite.handler.Config.RedisConnectionSetting = "localhost:6379"
	err := suite.handler.provisionRedisCache()

	// In this case, it will encounter a dial error because I don't
	// provide a running redis server.
	suite.Assert().Error(err)
}

type DetermineShouldCacheTestSuite struct {
	suite.Suite
	Config *Config
}

func (suite *DetermineShouldCacheTestSuite) SetupSuite() {
	if suite.Config == nil {
		suite.Config = getDefaultConfig()
	}
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
		shouldBeCached := shouldUseCache(req, suite.Config)
		suite.Equal(test.shouldBeCached, shouldBeCached)
	}

}

func (suite *DetermineShouldCacheTestSuite) TestNonGETOrHeadMethod() {
	r := httptest.NewRequest("POST", "/", nil)
	shouldBeCached := shouldUseCache(r, suite.Config)
	suite.False(shouldBeCached)
}

type DetermineShouldCachePOSTOnlyTestSuite struct {
	suite.Suite
	Config *Config
}

func (suite *DetermineShouldCachePOSTOnlyTestSuite) SetupSuite() {
	if suite.Config == nil {
		suite.Config = getDefaultConfig()
		suite.Config.MatchMethods = []string{"POST"}
	}
}

func (suite *DetermineShouldCachePOSTOnlyTestSuite) TestPOSTMethod() {
	r := httptest.NewRequest("POST", "/", nil)
	shouldBeCached := shouldUseCache(r, suite.Config)
	suite.True(shouldBeCached)
}

func (suite *DetermineShouldCachePOSTOnlyTestSuite) TestGETMethod() {
	r := httptest.NewRequest("GET", "/", nil)
	shouldBeCached := shouldUseCache(r, suite.Config)
	suite.False(shouldBeCached)
}

func TestCacheKeyTemplatingTestSuite(t *testing.T) {
	suite.Run(t, new(CacheKeyTemplatingTestSuite))
	suite.Run(t, new(DetermineShouldCacheTestSuite))
	suite.Run(t, new(DetermineShouldCachePOSTOnlyTestSuite))
	suite.Run(t, new(HandlerProvisionTestSuite))
}
