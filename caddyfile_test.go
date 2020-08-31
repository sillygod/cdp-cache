package httpcache

import (
	"testing"

	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/stretchr/testify/suite"
)

type CaddyfileTestSuite struct {
	suite.Suite
}

func (suite *CaddyfileTestSuite) TestSettingFileCacheType() {
	h := httpcaddyfile.Helper{
		Dispenser: caddyfile.NewTestDispenser(`
		http_cache {
			cache_type file
			match_path /assets
			lock_timeout 10m
			path /tmp/cache
			default_max_age 5m
			status_header "X-Cache-Status"
			cache_key "{http.request.method} {http.request.host}{http.request.uri.path} {http.request.uri.query}"
			cache_bucket_num 1024
			match_header Content-Type image/png
			match_header X-Forwarded-For 144.30.20.10
		}
		`),
	}
	handler, err := parseCaddyfile(h)
	suite.Nil(err)

	mh, ok := handler.(*Handler)
	suite.True(ok, "the caddyhttp middlewareHandler should be castable to Handler")
	suite.Equal(file, mh.Config.Type)

	suite.Equal(3, len(mh.Config.RuleMatchersRaws))
}

func (suite *CaddyfileTestSuite) TestErrorSetMultipleCacheType() {
	h := httpcaddyfile.Helper{
		Dispenser: caddyfile.NewTestDispenser(`
		http_cache {
			cache_type file in_memory
		}
		`),
	}
	_, err := parseCaddyfile(h)
	suite.Error(err, "it should raise the invalid usage of cache_type")
}

func (suite *CaddyfileTestSuite) TestInvalidCacheStatusHeader() {
	h := httpcaddyfile.Helper{
		Dispenser: caddyfile.NewTestDispenser(`
		http_cache {
			status_header X-Cache-Status A-Status	
		}`),
	}
	_, err := parseCaddyfile(h)
	suite.Error(err, "it should raise the invalid usage of status_header")
}

func (suite *CaddyfileTestSuite) TestInvalidParameter() {
	h := httpcaddyfile.Helper{
		Dispenser: caddyfile.NewTestDispenser(`
		http_cache {
			whocares haha
		}
		`),
	}
	_, err := parseCaddyfile(h)
	suite.Error(err, "invalid parameter")
}

func (suite *CaddyfileTestSuite) TestRedisConnectionSetting() {
	h := httpcaddyfile.Helper{
		Dispenser: caddyfile.NewTestDispenser(`
        http_cache {
            redis_connection_setting localhost:6379 2 pass 5
        }
        `),
	}

	_, err := parseCaddyfile(h)
	suite.Error(err, "invalid usage of redis_connection_setting in cache config.")

	h = httpcaddyfile.Helper{
		Dispenser: caddyfile.NewTestDispenser(`
        http_cache {
            redis_connection_setting localhost:6379 2
        }
        `),
	}

	_, err = parseCaddyfile(h)
	suite.Assert().NoError(err)

}

func (suite *CaddyfileTestSuite) TestDistributedCacheConfig() {
	h := httpcaddyfile.Helper{
		Dispenser: caddyfile.NewTestDispenser(`
		http_cache {
			distributed consul {
				service_name "cache_server"
			}
		}
		`),
	}

	_, err := parseCaddyfile(h)
	suite.Nil(err)
}

func TestCaddyfileTestSuite(t *testing.T) {
	suite.Run(t, new(CaddyfileTestSuite))
}
