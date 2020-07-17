package httpcache

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/stretchr/testify/suite"
)

func BenchmarkURLLock(b *testing.B) {
	c := getDefaultConfig()
	lock := NewURLLock(c)

	r := httptest.NewRequest("GET", "http://localhost:8000/test.txt", nil)
	repl := caddyhttp.NewTestReplacer(r)
	ctx := context.WithValue(r.Context(), caddy.ReplacerCtxKey, repl)
	r.WithContext(ctx)
	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		key := getKey(c.CacheKeyTemplate, r) // this function has some impact
		loc := lock.Acquire(key)
		loc.Unlock()
	}
}

type URLLockTestSuite struct {
	suite.Suite
}

func (suite *URLLockTestSuite) TestAcquireAndUnLock() {
	c := getDefaultConfig()
	lock := NewURLLock(c)
	l := lock.Acquire("hello")
	l.Unlock()
}

func TestURLLockTestSuite(t *testing.T) {
	suite.Run(t, new(URLLockTestSuite))
}
