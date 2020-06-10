package distributed

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/google/uuid"
	"github.com/hashicorp/consul/api"
	"github.com/sillygod/cdp-cache/backends"
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
	Srv        *http.Server
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if c.Srv != nil {
		if err := c.Srv.Shutdown(ctx); err != nil {
			return err
		}
	}

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

	healthURL := fmt.Sprintf("http://%s%s", ip.String(), c.Config.HealthURL)

	reg := &api.AgentServiceRegistration{
		ID:      idStr,
		Name:    "cache_server",
		Address: ip.String(),
		Port:    7777,
		Check: &api.AgentServiceCheck{
			TLSSkipVerify:                  true,
			Method:                         "GET",
			Timeout:                        "10s",
			Interval:                       "30s",
			HTTP:                           healthURL,
			Name:                           "health check for cache server",
			DeregisterCriticalServiceAfter: "15s",
		},
	}

	err = c.Client.Agent().ServiceRegister(reg)
	if err != nil {
		return err
	}

	errChan := make(chan error, 1)

	atch := backends.GetAutoCache()
	if atch != nil {
		mux := http.NewServeMux()
		mux.Handle("/_gp/", atch)
		c.Srv = &http.Server{
			// Addr:    ip.String(),
			Handler: mux,
		}

		go func() {
			if err := c.Srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				errChan <- err
			}
			fmt.Println("enter")
		}()
	}

	errChan <- nil

	// a routine to update the connection peers
	go func() {
		t := time.NewTicker(time.Second * 5)
		for {
			select {
			case <-t.C:
				peers, err := c.GetPeers()
				if err != nil {
					fmt.Println("fuck", err.Error())
				}

				atch := backends.GetAutoCache()
				atch.GroupcachePool.Set(peers...)
				fmt.Println("Peer: ", peers)

			}
		}
	}()

	err = <-errChan
	return err
}

func (c *ConsulService) GetPeers() ([]string, error) {

	result := []string{}

	name := "cache_server"
	serviceData, _, err := c.Client.Health().Service(name, "", true, &api.QueryOptions{})
	if err != nil {
		return nil, err
	}

	for _, entry := range serviceData {
		if entry.Service.Service != name {
			continue
		}

		result = append(result, fmt.Sprintf("%s", entry.Service.Address))
	}

	return result, nil
}

var (
	_ caddy.Provisioner  = (*ConsulService)(nil)
	_ caddy.CleanerUpper = (*ConsulService)(nil)
	_ caddy.Validator    = (*ConsulService)(nil)
)
