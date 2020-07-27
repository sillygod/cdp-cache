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
	caddy.RegisterModule(Writer{})
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

// Writer is a influxdb client to write time series data
type Writer struct {
	Client influxdb2.Client
	Config *Config
}

// CaddyModule returns the Caddy module information
func (Writer) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "caddy.logging.writers.influxlog",
		New: func() caddy.Module { return new(Writer) },
	}
}

// Validate currently do nothing
func (s *Writer) Validate() error {
	return nil
}

// Cleanup currently do nothing
func (s *Writer) Cleanup() error {
	s.Client.Close()
	return nil
}

// Provision currently do nothing
func (s *Writer) Provision(ctx caddy.Context) error {
	return nil
}

// String returns the expression of this struct
func (s *Writer) String() string {
	return "influxlog" + s.Config.Addr
}

// WriterKey returns a unique key representing this influxLogWriter
func (s *Writer) WriterKey() string {
	return "influxlog" + s.Config.Addr
}

// OpenWriter opens a new influxdb client with connection
func (s *Writer) OpenWriter() (io.WriteCloser, error) {
	// This is will be called at the StandardLibLog's provision
	client := influxdb2.NewClientWithOptions(
		s.Config.Addr, s.Config.Token, influxdb2.DefaultOptions())

	// set up the options here
	s.Client = client
	api := client.WriteAPI(s.Config.Organization, s.Config.Bucket)

	return &influxWriteCloser{api: api}, nil
}

var (
	_ caddy.Provisioner  = (*Writer)(nil)
	_ caddy.WriterOpener = (*Writer)(nil)
	_ caddy.CleanerUpper = (*Writer)(nil)
	_ caddy.Validator    = (*Writer)(nil)
)
