package proxyprotocol

import (
	"net"

	"github.com/caddyserver/caddy/v2"
	proxyproto "github.com/pires/go-proxyproto"
)

func init() {
	caddy.RegisterModule(ProxyProtocol{})
}

// ProxyProtocol implements the proxy protocol parser functions
type ProxyProtocol struct {
	Config *Config
}

// CaddyModule returns the Caddy module information
func (ProxyProtocol) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "caddy.listeners.proxy_protocol",
		New: func() caddy.Module { return new(ProxyProtocol) },
	}
}

// Provision initializes the proxy protocol
func (p *ProxyProtocol) Provision(ctx caddy.Context) error {
	if p.Config == nil {
		p.Config = getDefaultConfig()
	}

	return nil
}

// Validate checks the resource is set up correctly
func (p *ProxyProtocol) Validate() error {
	return nil
}

// Cleanup releases the holding resources
func (p *ProxyProtocol) Cleanup() error {
	return nil
}

// WrapListener wraps the net.Listener to customize the functionalities
func (p *ProxyProtocol) WrapListener(l net.Listener) net.Listener {
	pL := proxyproto.Listener{
		Listener:          l,
		ReadHeaderTimeout: p.Config.Timeout,
		Policy:            proxyproto.MustLaxWhiteListPolicy(p.Config.Allow),
	}

	return &pL
}

var (
	_ caddy.Provisioner     = (*ProxyProtocol)(nil)
	_ caddy.ListenerWrapper = (*ProxyProtocol)(nil)
	_ caddy.Module          = (*ProxyProtocol)(nil)
)
