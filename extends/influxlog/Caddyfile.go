package influxlog

import "github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"

const (
	keyAddr         = "addr"
	keyToken        = "token"
	keyOrganization = "organization"
	keyBucket       = "bucket"
)

// Config is the configuration for the influxLogWriter
type Config struct {
	Addr         string `json:"addr"`
	Token        string `json:"token"`
	Organization string `json:"organization"`
	Bucket       string `json:"bucket"`
}

func getDefaultConfig() *Config {
	return &Config{
		Addr:  "localhost:9999",
		Token: "",
	}
}

// UnmarshalCaddyfile deserializes Caddyfile tokens into influxlog config
// influxlog {
//   addr
//   token
// 	 organization
//   bucket
// }
func (s *Writer) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
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

			case keyOrganization:
				config.Organization = args[0]

			case keyBucket:
				config.Bucket = args[0]

			default:
				return d.Errf("unrecognized subdirective %s", parameter)

			}

		}
	}

	s.Config = config
	return nil
}

var (
	_ caddyfile.Unmarshaler = (*Writer)(nil)
)
