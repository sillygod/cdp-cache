package httpcache

import (
	"net/http"
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

	suite.Equal("GET example.com/songa age=20&class=A", result)

}

func TestCacheKeyTemplatingTestSuite(t *testing.T) {
	suite.Run(t, new(CacheKeyTemplatingTestSuite))
}
