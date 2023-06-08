package proxyprotocol

import (
	"fmt"
	"time"

	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

var (
	defaultTimeout = 5 * time.Second
	defaultAllow   = []string{"0.0.0.0/0"}
)

const (
	keyTimeout = "timeout"
	keyAllow   = "allow"
)

// Config is the configuration for proxyprotocol
type Config struct {
	Timeout time.Duration `json:"timeout,omitempty"`
	Allow   []string      `json:"allow,omitempty"`
}

func getDefaultConfig() *Config {
	return &Config{
		Timeout: defaultTimeout,
		Allow:   defaultAllow,
	}
}

// UnmarshalCaddyfile deserialize Caddyfiles tokens into proxyprotocol's config
//
//	proxy_protocol {
//	  allow 0.0.0.0/0
//	  timeout 5s
//	}
func (p *ProxyProtocol) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	config := getDefaultConfig()

	for d.Next() {

		for d.NextBlock(0) {
			parameter := d.Val()
			args := d.RemainingArgs()

			switch parameter {
			case keyTimeout:
				duration, err := time.ParseDuration(args[0])
				if err != nil {
					return d.Err(fmt.Sprintf("%s:%s , %s", keyTimeout, "invalid duration", args[0]))
				}
				config.Timeout = duration
			case keyAllow:
				config.Allow = args
			default:
				return d.Errf("unrecognized subdirective %s", parameter)
			}
		}
	}

	p.Config = config
	return nil
}

var (
	_ caddyfile.Unmarshaler = (*ProxyProtocol)(nil)
)
