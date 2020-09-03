package distributed

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/google/uuid"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/api/watch"
	"github.com/sillygod/cdp-cache/backends"
	"github.com/sillygod/cdp-cache/pkg/helper"
)

var (
	l        sync.Mutex
	client   *api.Client
	handlers []WatchHandler
)

func init() {
	caddy.RegisterModule(ConsulService{})
}

// WatchHandler holds callback and its params. This will be registered on
// the consul's watch event.
type WatchHandler struct {
	Pg       map[string]interface{}
	Callback WatchCallback
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

	// Keyprefix is the key prefix for listening key changes
	Keyprefix = "del_cache_key"
)

// WatchCallback is a processor for watch event
type WatchCallback func(data interface{}) error

// DistributedLock holds a session and a lock for mutually exclusive usage.
type DistributedLock struct {
	Session   *api.Session
	SessionID string
	lock      *api.Lock
	client    *api.Client
}

// Lock acquires distributed lock
func (d *DistributedLock) Lock() (<-chan struct{}, error) {
	return d.lock.Lock(nil)
}

// Unlock release the distributed lock
func (d *DistributedLock) Unlock() error {
	return d.lock.Unlock()
}

// Destroy release the session
func (d *DistributedLock) Destroy() {
	d.Session.Destroy(d.SessionID, nil)
}

// NewDistributedLock new a distributed lock based on consul session
func NewDistributedLock(key string) (*DistributedLock, error) {
	session := client.Session()
	key = Keyprefix + "/" + key

	se := &api.SessionEntry{
		Name:     "_deleted_keys_manager",
		TTL:      api.DefaultLockSessionTTL,
		Behavior: api.SessionBehaviorDelete,
	}

	id, _, err := session.Create(se, nil)
	if err != nil {
		return nil, err
	}

	ip, _ := helper.IPAddr()

	opts := &api.LockOptions{
		Key:          key,
		Value:        []byte(ip.String()),
		Session:      id,
		SessionName:  se.Name,
		SessionTTL:   se.TTL,
		LockWaitTime: 1 * time.Second,
		LockTryOnce:  true,
	}

	lock, err := client.LockOpts(opts)
	if err != nil {
		return nil, err
	}

	return &DistributedLock{
		Session:   session,
		SessionID: id,
		lock:      lock,
		client:    client,
	}, nil

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

func genServiceID(serviceName string, ip string) string {
	uid := uuid.New()
	idStr := fmt.Sprintf("%s:%s:%s", serviceName, ip, uid.String())
	return idStr
}

func getIPFromServiceID(serviceID string) string {
	return strings.Split(serviceID, ":")[1]
}

// RegisterWatchHandler append a pair of watch and handler to the list
// which will be registered to the consul watch service.
func RegisterWatchHandler(wh WatchHandler) {
	handlers = append(handlers, wh)
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

	client = consulClient
	c.Client = consulClient
	c.Catalog = c.Client.Catalog()

	c.KV = c.Client.KV()

	ip, err := helper.IPAddr()
	if err != nil {
		return err
	}

	idStr := genServiceID(c.Config.ServiceName, ip.String())
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
	return c.RegisterWatches()
}

// RegisterWatches registers the specified watches
func (c *ConsulService) RegisterWatches() error {
	var err error

	handlers = append(handlers, []WatchHandler{
		{
			Pg:       GetChecksParams(c.Config.ServiceName),
			Callback: c.handleChecks,
		},
		{
			Pg:       GetKeysParams("caddy_config"),
			Callback: c.handleConfigChanged,
		},
	}...)

	for _, h := range handlers {
		err = c.RegisterWatch(h.Pg, h.Callback)
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *ConsulService) handleConfigChanged(data interface{}) error {
	kv, ok := data.(*api.KVPair)
	if !ok {
		return fmt.Errorf("non expected data type: %s", reflect.TypeOf(data))
	}

	// presume the values is json format
	return caddy.Load(kv.Value, false)
}

func (c *ConsulService) handleChecks(data interface{}) error {
	peers := []string{}

	checks, ok := data.([]*api.HealthCheck)
	if !ok {
		return fmt.Errorf("non expected data type: %s", reflect.TypeOf(data))
	}

	for _, check := range checks {

		if check.Status == "passing" {
			peerIP := getIPFromServiceID(check.ServiceID)
			address := fmt.Sprintf("http://%s", peerIP)
			peers = append(peers, address)
		}
	}

	pool := backends.GetGroupCachePool()
	pool.Set(peers...)
	caddy.Log().Named("distributed cache").Debug(fmt.Sprintf("Peers: %s", peers))

	return nil
}

// GetKeyPrefixParams gets the params for watching prefix event
func GetKeyPrefixParams(keyPrefix string) map[string]interface{} {
	params := GetParams(keyPrefixWatchKind)
	params["prefix"] = keyPrefix
	return params
}

// GetKeysParams gets the params for watching key event
func GetKeysParams(key string) map[string]interface{} {
	params := GetParams(keyWatchKind)
	params["key"] = key
	return params
}

// GetChecksParams gets the params for watching checks event
func GetChecksParams(serviceName string) map[string]interface{} {
	params := GetParams(checksWatchKind)
	params["service"] = serviceName
	return params
}

// GetParams returns the common params for all watch events
func GetParams(kind watchKind) map[string]interface{} {
	params := map[string]interface{}{}
	params["type"] = string(kind) // consul will ensure the type to be string
	return params
}

// RegisterWatch watches the events and attaches the handlers to them
func (c *ConsulService) RegisterWatch(params map[string]interface{}, fn WatchCallback) error {
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
