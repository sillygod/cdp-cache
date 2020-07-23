package influxlog

import (
	"fmt"
	"io"
	"time"

	"github.com/caddyserver/caddy/v2"
	influxdb2 "github.com/influxdata/influxdb-client-go"
	influxdb2api "github.com/influxdata/influxdb-client-go/api"
)

func init() {
	caddy.RegisterModule(InfluxLogWriter{})
}

type influxWriteCloser struct {
	api influxdb2api.WriteAPI
}

func (i *influxWriteCloser) Write(b []byte) (int, error) {
	n := len(b)
	content := string(b)
	// TODO: parse the content {time} {level} {module} {messages}
	// Remember to rewrite the function below in next PR.
	fmt.Println(content)
	tags := map[string]string{"ip": "135.22.5.3"}
	fields := map[string]interface{}{"Country": "HI"}
	p := influxdb2.NewPoint("syslog", tags, fields, time.Now())
	i.api.WritePoint(p)
	return n, nil
}

func (i *influxWriteCloser) Close() error {
	i.api.Flush()
	i.api.Close()
	return nil
}

type InfluxLogWriter struct {
	Client influxdb2.Client
	Config *Config
}

func (InfluxLogWriter) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "caddy.logging.writers.influxlog",
		New: func() caddy.Module { return new(InfluxLogWriter) },
	}
}

func (s *InfluxLogWriter) Validate() error {
	return nil
}

func (s *InfluxLogWriter) Cleanup() error {
	s.Client.Close()
	return nil
}

func (s *InfluxLogWriter) Provision(ctx caddy.Context) error {
	return nil
}

func (s *InfluxLogWriter) String() string {
	return "influxlog" + s.Config.Addr
}

// WriterKey returns a unique key representing this influxLogWriter
func (s *InfluxLogWriter) WriterKey() string {
	return "influxlog" + s.Config.Addr
}

// OpenWriter opens a new influxdb client with connection
func (s *InfluxLogWriter) OpenWriter() (io.WriteCloser, error) {
	// This is will be called at the StandardLibLog's provision
	client := influxdb2.NewClientWithOptions(
		s.Config.Addr, s.Config.Token, influxdb2.DefaultOptions())

	// set up the options here
	s.Client = client
	api := client.WriteAPI(s.Config.Organization, s.Config.Bucket)

	return &influxWriteCloser{api: api}, nil
}

var (
	_ caddy.Provisioner  = (*InfluxLogWriter)(nil)
	_ caddy.WriterOpener = (*InfluxLogWriter)(nil)
	_ caddy.CleanerUpper = (*InfluxLogWriter)(nil)
	_ caddy.Validator    = (*InfluxLogWriter)(nil)
)
