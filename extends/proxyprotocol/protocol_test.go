package proxyprotocol

import (
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/stretchr/testify/suite"
)

type ProxyProtocolTestSuite struct {
	suite.Suite
	pr *ProxyProtocol
}

func (p *ProxyProtocolTestSuite) initSuite() error {
	h := httpcaddyfile.Helper{
		Dispenser: caddyfile.NewTestDispenser(`
        {
            proxy_protocol {
                timeout 5s
            }
        }
        `),
	}

	p.pr = new(ProxyProtocol)

	if err := p.pr.UnmarshalCaddyfile(h.Dispenser); err != nil {
		panic(err)
	}

	if err := p.pr.Provision(caddy.Context{}); err != nil {
		return err
	}

	return nil
}

func (p *ProxyProtocolTestSuite) SetupSuite() {
	p.initSuite()
}

func (p *ProxyProtocolTestSuite) TestProxyProtocol() {
    p.Equal(p.pr.Config.Timeout, time.Second*5)
}

func (p *ProxyProtocolTestSuite) TearDownSuite() {

}

func TestProxyProtocolTestSuite(t *testing.T) {
	suite.Run(t, new(ProxyProtocolTestSuite))
}
