package distributed

import (
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

const (
	keyServiceName = "service_name"
	keyAddr        = "addr"
	keyHealthCheck = "health_check"
	keyToken       = "token"
)

// Config is the configuration for the consul
type Config struct {
	ServiceName string `json:"service_name,omitempty"`
	Addr        string `json:"addr,omitempty"`
	HealthURL   string `json:"health_url,omitempty"`
	Token       string `json:"token,omitempty"`
}

func getDefaultConfig() *Config {
	return &Config{
		ServiceName: "",
		Addr:        "localhost:8500",
	}
}

// UnmarshalCaddyfile deserializes Caddyfile tokens into consulservice's config
//
//	distributed consul {
//	  service_name
//	  addr
//	  token
//	  health_url
//	}
func (c *ConsulService) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	config := getDefaultConfig()

	for d.Next() {
		for d.NextBlock(0) {

			parameter := d.Val()
			args := d.RemainingArgs()

			switch parameter {
			case keyAddr:
				config.Addr = args[0]

			case keyServiceName:
				config.ServiceName = args[0]

			case keyHealthCheck:
				config.HealthURL = args[0]

			case keyToken:
				config.Token = args[0]

			default:
				return d.Errf("unrecognized subdirective %s", parameter)

			}
		}
	}

	c.Config = config

	return nil
}

var (
	_ caddyfile.Unmarshaler = (*ConsulService)(nil)
)
