package distributed

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/caddyserver/caddy/v2"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/api/watch"
	"github.com/sillygod/cdp-cache/backends"
	"github.com/sillygod/cdp-cache/pkg/helper"
)

var (
	l sync.Mutex
)

func init() {
	caddy.RegisterModule(ConsulService{})
}

type watchKind string

// type specifies which watch func to be used.
// Refer from the part of the consul's source code.

// watchFuncFactory = map[string]watchFactory{
//  "key":           keyWatch,
// 	"keyprefix":     keyPrefixWatch,
// 	"services":      servicesWatch,
// 	"nodes":         nodesWatch,
// 	"service":       serviceWatch,
// 	"checks":        checksWatch,
// 	"event":         eventWatch,
// 	"connect_roots": connectRootsWatch,
// 	"connect_leaf":  connectLeafWatch,
// 	"agent_service": agentServiceWatch,
// }

const (
	keyWatchKind          watchKind = "key"
	keyPrefixWatchKind    watchKind = "keyprefix"
	servicesWatchKind     watchKind = "services"
	nodeWatchKind         watchKind = "nodes"
	serviceWatchKind      watchKind = "service"
	checksWatchKind       watchKind = "checks"
	eventWatchKind        watchKind = "event"
	connectRootsWatchKind watchKind = "connect_roots"
	connectLeafWatchKind  watchKind = "connect_leaf"
	agentServiceWatchKind watchKind = "agent_service"
)

type watchCallback func(data interface{}) error

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
	config.Token = c.Config.Token

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

	idStr := fmt.Sprintf("%s:%s", c.Config.ServiceName, ip.String())
	c.ServiceIDs = append(c.ServiceIDs, idStr)

	healthURL := fmt.Sprintf("http://%s%s", ip.String(), c.Config.HealthURL)

	reg := &api.AgentServiceRegistration{
		ID:      idStr,
		Name:    c.Config.ServiceName,
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

	// Register some watch functions ex. health check, key-value store etc.
	err = c.RegisterWatches()
	if err != nil {
		return err
	}

	return nil
}

// RegisterWatches registers the specified watches
func (c *ConsulService) RegisterWatches() error {
	var err error

	handlers := []struct {
		kind     watchKind
		pg       map[string]interface{}
		callback watchCallback
	}{
		{
			kind:     checksWatchKind,
			pg:       c.getParams(checksWatchKind),
			callback: c.handleChecks,
		},
		{
			kind:     keyWatchKind,
			pg:       c.getKeysParams("caddy_config"),
			callback: c.handleConfigChanged,
		},
		{
			kind:     keyPrefixWatchKind,
			pg:       c.getKeyPrefixParams("cache_key"),
			callback: c.handleDeleteKey,
		},
	}

	for _, h := range handlers {
		err = c.RegisterWatch(h.kind, h.pg, h.callback)
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *ConsulService) handleDeleteKey(data interface{}) error {
	return nil
}

func (c *ConsulService) handleConfigChanged(data interface{}) error {
	return nil
}

func (c *ConsulService) handleChecks(data interface{}) error {
	peers := []string{}

	checks, ok := data.([]*api.HealthCheck)
	if !ok {
		return fmt.Errorf("non expected data type: %s", reflect.TypeOf(data))
	}

	for _, check := range checks {

		if check.Status == "passing" {
			peer_ip := strings.Split(check.ServiceID, ":")[1]
			address := fmt.Sprintf("http://%s", peer_ip)
			peers = append(peers, address)
		}
	}

	pool := backends.GetGroupCachePool()
	pool.Set(peers...)
	caddy.Log().Named("distributed cache").Debug(fmt.Sprintf("Peers: %s", peers))

	return nil
}

func (c *ConsulService) getKeyPrefixParams(keyPrefix string) map[string]interface{} {
	params := c.getParams(keyPrefixWatchKind)
	params["keyprefix"] = keyPrefix
	return params
}

func (c *ConsulService) getKeysParams(key string) map[string]interface{} {
	params := c.getParams(keyWatchKind)
	params["key"] = key
	return params
}

func (c *ConsulService) getParams(kind watchKind) map[string]interface{} {
	params := map[string]interface{}{}
	params["type"] = kind
	params["service"] = c.Config.ServiceName
	return params
}

func (c *ConsulService) RegisterWatch(kind watchKind, params map[string]interface{}, fn watchCallback) error {
	plan, err := watch.Parse(params)
	if err != nil {
		return err
	}

	// the result will vary with the different the watch type
	plan.Handler = func(index uint64, result interface{}) {
		fn(result)
	}

	go func() {
		err := plan.Run(c.Config.Addr)
		if err != nil {
			plan.Stop()
		}
	}()

	return nil
}

// GetPeers get the peers in the same cluster
func (c *ConsulService) GetPeers() ([]string, error) {

	peerMap := make(map[string]struct{})
	peers := []string{}

	name := c.Config.ServiceName
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
