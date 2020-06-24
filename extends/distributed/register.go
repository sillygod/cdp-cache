package distributed

import (
	"fmt"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/hashicorp/consul/api"
	"github.com/sillygod/cdp-cache/backends"
	"github.com/sillygod/cdp-cache/pkg/helper"
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

// Validate checks the resource is set up correctly
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

// Provision init the consul's agent and establish connection
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

	ip, err := helper.IPAddr()
	if err != nil {
		return err
	}

	idStr := "cache_server:" + ip.String()
	c.ServiceIDs = append(c.ServiceIDs, idStr)

	healthURL := fmt.Sprintf("http://%s%s", ip.String(), c.Config.HealthURL)

	reg := &api.AgentServiceRegistration{
		ID:      idStr,
		Name:    "cache_server",
		Address: ip.String(),
		Port:    7777,
		Check: &api.AgentServiceCheck{
			TLSSkipVerify:                  true,
			Method:                         "GET",
			Timeout:                        "3s",
			Interval:                       "10s",
			HTTP:                           healthURL,
			Name:                           "health check for cache server",
			DeregisterCriticalServiceAfter: "15s",
		},
	}

	err = c.Client.Agent().ServiceRegister(reg)
	if err != nil {
		return err
	}

	// a routine to update the connection peers
	// TODO: research about the consul's event maybe we can use it
	// to replace this routine
	go func() {
		t := time.NewTicker(time.Second * 20)
		for {
			select {
			case <-t.C:
				peers, err := c.GetPeers()
				if err != nil {
					caddy.Log().Named("distributed cache").Error(fmt.Sprintf("get peer error: %s", err.Error()))
				}

				pool := backends.GetGroupCachePool()
				pool.Set(peers...)
				caddy.Log().Named("distributed cache").Debug(fmt.Sprintf("Peers: %s", peers))
			}
		}
	}()

	return nil
}

// GetPeers get the peers in the same cluster
func (c *ConsulService) GetPeers() ([]string, error) {

	peerMap := make(map[string]struct{})
	peers := []string{}

	name := "cache_server"
	serviceData, _, err := c.Client.Health().Service(name, "", true, &api.QueryOptions{})
	if err != nil {
		return nil, err
	}

	for _, entry := range serviceData {
		if entry.Service.Service != name {
			continue
		}

		address := fmt.Sprintf("http://%s", entry.Service.Address)
		if _, ok := peerMap[address]; !ok {
			peerMap[address] = struct{}{}
			peers = append(peers, address)
		}
	}

	return peers, nil
}

var (
	_ caddy.Provisioner  = (*ConsulService)(nil)
	_ caddy.CleanerUpper = (*ConsulService)(nil)
	_ caddy.Validator    = (*ConsulService)(nil)
)
