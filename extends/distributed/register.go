package distributed

import (
	"sync"

	"github.com/caddyserver/caddy/v2"
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
	Client  *api.Client
	KV      *api.KV
	Catalog *api.Catalog
	Config  *Config
}

// CaddyModule returns the Caddy module information
func (ConsulService) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "distributed.consul",
		New: func() caddy.Module { return new(ConsulService) },
	}
}

func (c *ConsulService) Validate() error {
	return nil
}

func (c *ConsulService) Cleanup() error {
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

	// api.AgentServiceRegistration{}
	// acquireKV := &api.KVPair{}
	// I don't know what's the Session effect.
	// kv = api.KV()
	// kv.Get()
	// api.SessionEntry{}
	// consulClient.Agent().ServiceDeregister() to unregistered service

	// svc, _ := connect.NewService(c.Config.ServiceName, c.Client)

	reg := &api.AgentServiceRegistration{
		ID:    "cache_server",
		Name:  "cache_server",
		Port:  2019,
		Check: &api.AgentServiceCheck{},
	}

	err = c.Client.Agent().ServiceRegister(reg)
	if err != nil {
		return err
	}

	// c.Catalog.NodeServiceList(node string, q *api.QueryOptions)
	// c.Catalog.Service(service string, tag string, q *api.QueryOptions)
	// c.Catalog.Services(&opts)

	// TODO: check the service
	// add some functions
	// - health check
	// - unregister the service from the agent
	// - get the peer list from the service name (attach these to the groupcache's pool)

	// NOTE: what the fuck how to get the list of service

	return nil
}

var (
	_ caddy.Provisioner = (*ConsulService)(nil)
	_ caddy.Validator   = (*ConsulService)(nil)
)
