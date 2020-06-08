package distributed

import (
	"fmt"
	"net"
	"sync"

	"github.com/caddyserver/caddy/v2"
	"github.com/google/uuid"
	"github.com/hashicorp/consul/api"
)

var (
	l sync.Mutex
)

func init() {
	caddy.RegisterModule(ConsulService{})

}

// ConsulService handles the client to interact with the consul agent
type ConsulService struct {
	Client     *api.Client
	KV         *api.KV
	Catalog    *api.Catalog
	Config     *Config
	ServiceIDs []string
}

// CaddyModule returns the Caddy module information
func (ConsulService) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "distributed.consul",
		New: func() caddy.Module { return new(ConsulService) },
	}
}

// ipAddr get the local ip address
func ipAddr() (net.IP, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.IsGlobalUnicast() {
			if ipnet.IP.To4() != nil || ipnet.IP.To16() != nil {
				return ipnet.IP, nil
			}
		}
	}
	return nil, nil
}

func (c *ConsulService) Validate() error {
	return nil
}

// Cleanup releases the holding resources
func (c *ConsulService) Cleanup() error {
	// TODO: Is there anywhere to distinguish reload or shutdown
	for _, id := range c.ServiceIDs {
		if err := c.Client.Agent().ServiceDeregister(id); err != nil {
			return err
		}
	}
	return nil
}

func (c *ConsulService) Provision(ctx caddy.Context) error {
	// init the consul api client here
	if c.Config == nil {
		c.Config = getDefaultConfig()
	}

	l.Lock()
	defer l.Unlock()

	config := api.DefaultConfig()
	config.Address = c.Config.Addr

	consulClient, err := api.NewClient(config)
	if err != nil {
		return err
	}

	c.Client = consulClient
	c.Catalog = c.Client.Catalog()
	c.KV = c.Client.KV()

	id := uuid.New()
	idStr := id.String()
	c.ServiceIDs = append(c.ServiceIDs, idStr)

	ip, err := ipAddr()
	if err != nil {
		return err
	}

	// TODO: find a way to get admin server's port
	healthURL := fmt.Sprintf("http://%s%s", ip.String(), c.Config.HealthURL)
	reg := &api.AgentServiceRegistration{
		ID:   idStr,
		Name: "cache_server",
		Port: 7777,
		Check: &api.AgentServiceCheck{
			TLSSkipVerify: true,
			Method:        "GET",
			Timeout:       "10s",
			Interval:      "30s",
			HTTP:          healthURL,
			Name:          "health check for cache server",
		},
	}

	err = c.Client.Agent().ServiceRegister(reg)
	if err != nil {
		return err
	}

	// c.Catalog.NodeServiceList(node string, q *api.QueryOptions)
	// c.Catalog.Service(service string, tag string, q *api.QueryOptions)
	// c.Catalog.Services(&opts)

	return nil
}

func (c *ConsulService) GetPeers() {
	// TODO:
	// get the peer list from the service name (attach these to the groupcache's pool)

}

var (
	_ caddy.Provisioner  = (*ConsulService)(nil)
	_ caddy.CleanerUpper = (*ConsulService)(nil)
	_ caddy.Validator    = (*ConsulService)(nil)
)
