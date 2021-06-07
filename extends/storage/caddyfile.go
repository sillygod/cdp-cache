package mystorage

import (
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

const (
	keyAddr      = "addr"
	keyToken     = "token"
	keyKeyPrefix = "key_prefix"
)

// Config is the configuration for consul storage
type Config struct {
	Addr      string `json:"addr.omitempty"`
	Token     string `json:"token,omitempty"`
	KeyPrefix string `json:"key_prefix,omitempty"`
}

func getDefaultConfig() *Config {
	return &Config{
		KeyPrefix: "_consul_cert_",
		Addr:      "localhost:8500",
	}
}

// UnmarshalCaddyfile deserialize Caddyfile tokens into consul storage's config
// storage consul {
//    addr
//    token
//    key_prefix
// }
func (s *Storage) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	config := getDefaultConfig()

	for d.Next() {
		for d.NextBlock(0) {
			parameter := d.Val()
			args := d.RemainingArgs()

			switch parameter {
			case keyAddr:
				config.Addr = args[0]
			case keyToken:
				config.Token = args[0]
			case keyKeyPrefix:
				config.KeyPrefix = args[0]

			default:
				return d.Errf("unrecognized subdirective %s", parameter)

			}

		}
	}
	s.Config = config
	return nil
}

var (
	_ caddyfile.Unmarshaler = (*Storage)(nil)
)
