package influxlog

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/sillygod/cdp-cache/pkg/helper"

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

func (i *influxWriteCloser) write(b []byte) (int, error) {
	// TODO: consider to do this in background worker
	n := len(b)
	content := string(b)
	// parse the content {time} {level} {module} {action} {messages}
	caddy.Log().Named("influxlog will write").Debug(content)

	var ts time.Time
	var err error

	tags := map[string]string{}
	fields := map[string]interface{}{}
	tokens := strings.Split(content, "\t")

	if len(tokens) == 5 {
		ts, err = time.Parse(helper.LogUTCTimeFormat, tokens[0])
		if err != nil {
			ts = time.Now()
		}
		err := json.Unmarshal([]byte(tokens[4]), &fields)
		fmt.Println(tokens[4])
		if err != nil {
			fmt.Println(err)
			return 0, err
		}
		fields["message"] = fmt.Sprintf("%s %s", tokens[3], tokens[4])
	}

	p := influxdb2.NewPoint("syslog", tags, fields, ts)
	i.api.WritePoint(p)
	return n, nil
}

func (i *influxWriteCloser) Write(b []byte) (int, error) {
	return i.write(b)
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
