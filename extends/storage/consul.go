package mystorage

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/certmagic"
	"github.com/hashicorp/consul/api"
)

func init() {
	caddy.RegisterModule(Storage{})
}

// Storage implements the certmagic storage's interface
// This holds the consul client and kv store
type Storage struct {
	Client *api.Client
	locks  map[string]*api.Lock
	KV     *api.KV
	Config *Config
}

// CaddyModule returns the Caddy module information
func (Storage) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "caddy.storage.consul",
		New: func() caddy.Module { return new(Storage) },
	}
}

// CertMagicStorage transforms storage to certmagic.Storage
func (s *Storage) CertMagicStorage() (certmagic.Storage, error) {
	return s, nil
}

// Provision initializes the storage
func (s *Storage) Provision(ctx caddy.Context) error {
	if s.Config == nil {
		s.Config = getDefaultConfig()
	}

	config := api.DefaultConfig()
	config.Address = s.Config.Addr
	config.Token = s.Config.Token

	client, err := api.NewClient(config)
	if err != nil {
		return err
	}

	s.Client = client
	if _, err := s.Client.Agent().NodeName(); err != nil {
		return fmt.Errorf("err: %s, unable to ping consul", err.Error())
	}

	s.KV = s.Client.KV()
	s.locks = make(map[string]*api.Lock)

	return nil
}

// Validate checks the resource is set up correctly
func (s *Storage) Validate() error {
	return nil
}

// Cleanup releases the holding resources
func (s *Storage) Cleanup() error {
	return nil
}

func (s *Storage) generateKey(key string) string {
	// https://www.consul.io/commands/kv/get
	return path.Join(s.Config.KeyPrefix, key)
}

// Store stores the key into consul's kv store
func (s *Storage) Store(ctx context.Context, key string, value []byte) error {
	kv := &api.KVPair{Key: s.generateKey(key), Value: value}

	if _, err := s.KV.Put(kv, nil); err != nil {
		return fmt.Errorf("unable to store data: %s, key: %s", err.Error(), key)
	}

	return nil
}

// Load retrieves the value at key.
func (s *Storage) Load(ctx context.Context, key string) ([]byte, error) {
	kv, _, err := s.KV.Get(s.generateKey(key), &api.QueryOptions{RequireConsistent: true})
	if err != nil {
		return nil, fmt.Errorf("unable to get data: %s, key: %s", err.Error(), s.generateKey(key))
	}

	if kv == nil {
		return nil, fmt.Errorf("key: %s does not exist", s.generateKey(key))
	}

	return kv.Value, nil
}

// Delete deletes key. An error should be
// returned only if the key still exists
// when the method returns.
func (s *Storage) Delete(ctx context.Context, key string) error {
	kv, _, err := s.KV.Get(s.generateKey(key), &api.QueryOptions{RequireConsistent: true})
	if err != nil {
		return fmt.Errorf("unable to get data: %s, key: %s", err.Error(), s.generateKey(key))
	}

	success, _, err := s.KV.DeleteCAS(kv, nil)
	if err != nil {
		return fmt.Errorf("unable to delete data: %s, key: %s", err.Error(), s.generateKey(key))
	}

	if !success {
		return fmt.Errorf("failed to delete data, key: %s", s.generateKey(key))
	}

	return nil
}

// Exists returns true if the key exists
// and there was no error checking.
func (s *Storage) Exists(ctx context.Context, key string) bool {
	kv, _, err := s.KV.Get(s.generateKey(key), &api.QueryOptions{RequireConsistent: true})
	return kv != nil && err == nil
}

// List returns all keys that match prefix.
// If recursive is true, non-terminal keys
// will be enumerated (i.e. "directories"
// should be walked); otherwise, only keys
// prefixed exactly by prefix will be listed.
func (s *Storage) List(ctx context.Context, prefix string, recursive bool) ([]string, error) {
	resultKeys := []string{}

	keys, _, err := s.KV.Keys(s.generateKey(prefix), "", &api.QueryOptions{RequireConsistent: true})
	if err != nil {
		return resultKeys, err
	}

	if len(keys) == 0 {
		return resultKeys, fmt.Errorf("no key at %s", prefix)
	}

	if recursive {
		resultKeys = append(resultKeys, keys...)
		return resultKeys, nil
	}

	// process non-recursive result
	keyMaps := map[string]struct{}{}
	for _, key := range keys {
		dir := strings.Split(strings.TrimPrefix(key, prefix+"/"), "/")
		keyMaps[dir[0]] = struct{}{}
	}

	for key := range keyMaps {
		resultKeys = append(resultKeys, path.Join(prefix, key))
	}

	return resultKeys, nil
}

// Stat returns information about key.
func (s *Storage) Stat(ctx context.Context, key string) (certmagic.KeyInfo, error) {
	kv, _, err := s.KV.Get(s.generateKey(key), &api.QueryOptions{RequireConsistent: true})
	if err != nil {
		return certmagic.KeyInfo{}, fmt.Errorf("unable to get data: %s, key: %s", err.Error(), s.generateKey(key))
	}

	if kv == nil {
		return certmagic.KeyInfo{}, fmt.Errorf("key: %s does not exist", s.generateKey(key))
	}

	// what will happend if I don't give the modified time
	return certmagic.KeyInfo{
		Key:        key,
		Size:       int64(len(kv.Value)),
		IsTerminal: false,
	}, nil
}

// Lock locks key
func (s *Storage) Lock(ctx context.Context, key string) error {
	if _, exists := s.locks[key]; exists {
		return nil
	}

	lock, err := s.Client.LockKey(s.generateKey(key))
	if err != nil {
		return fmt.Errorf("err: %s, could not create lock for key: %s", err.Error(), s.generateKey(key))
	}

	lockCh, err := lock.Lock(ctx.Done())
	if err != nil {
		return fmt.Errorf("err: %s, unable to lock: %s", err.Error(), s.generateKey(key))
	}

	s.locks[key] = lock

	go func() {
		<-lockCh
		s.Unlock(ctx, key)
	}()

	return nil
}

// Unlock unlocks key
func (s *Storage) Unlock(ctx context.Context, key string) error {
	lock, exists := s.locks[key]
	if !exists {
		return fmt.Errorf("lock key: %s not found", s.generateKey(key))
	}

	err := lock.Unlock()
	if err != nil {
		return fmt.Errorf("unable to unlock: %s, key: %s", err.Error(), s.generateKey(key))
	}

	delete(s.locks, key)
	return nil
}

var (
	_ caddy.Provisioner      = (*Storage)(nil)
	_ caddy.CleanerUpper     = (*Storage)(nil)
	_ caddy.Validator        = (*Storage)(nil)
	_ certmagic.Storage      = (*Storage)(nil)
	_ certmagic.Locker       = (*Storage)(nil)
	_ caddy.StorageConverter = (*Storage)(nil)
)
