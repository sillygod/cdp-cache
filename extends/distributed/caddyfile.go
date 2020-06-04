package distributed

import (
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

// Config is the configuration for the consul
type Config struct {
	ServiceName string `json:"service_name,omitempty"`
	Addr        string `json:"addr,omitempty"`
}

func getDefaultConfig() *Config {
	return &Config{
		ServiceName: "",
		Addr:        "localhost:8500",
	}
}

// UnmarshalCaddyfile deserializes Caddyfile tokens into caddy cache's Handler
// distributed consul {
//   service_name
//   addr
// }
func (c *ConsulService) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	config := getDefaultConfig()

	for d.Next() {
		for d.NextBlock(0) {

			parameter := d.Val()
			args := d.RemainingArgs()

			switch parameter {
			case "addr":
				config.Addr = args[0]

			case "service_name":
				config.ServiceName = args[0]

			default:
				return d.Errf("unrecognized subdirective %s", d.Val())

			}
		}
	}

	c.Config = config

	return nil
}

var (
	_ caddyfile.Unmarshaler = (*ConsulService)(nil)
)
