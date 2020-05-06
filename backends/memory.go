package backends

import (
	"bytes"
	"context"
	"io"
	"io/ioutil"
	"sync"

	"github.com/golang/groupcache"
	"github.com/pomerium/autocache"
)

type ctxKey string

const getterCtxKey ctxKey = "getter"

var (
	groupName = "http_cache"
	pool      *groupcache.HTTPPool
	atch      *autocache.Autocache
	groupch   *groupcache.Group
	l         sync.Mutex
)

// InMemoryBackend saves the content into inmemory with
// groupcache.
type InMemoryBackend struct {
	Group            *groupcache.Group
	Ctx              context.Context
	CacheKeyTemplate string
	cachedBytes      []byte
}

// InitGroupCacheRes init the resources for groupcache
// init this in the handler provision stage.
func InitGroupCacheRes(maxSize int) error {
	var err error

	l.Lock()
	defer l.Unlock()

	if atch == nil {
		atch, err = autocache.New(&autocache.Options{})
		if err != nil {
			return err
		}
		_, err = atch.Join(nil)
	}

	if pool == nil {
		pool = atch.GroupcachePool
	}

	if groupch == nil {
		groupch = groupcache.NewGroup(groupName, int64(maxSize), groupcache.GetterFunc(getter))
	}

	return nil
}

func getter(ctx context.Context, key string, dest groupcache.Sink) error {
	p := ctx.Value(getterCtxKey).([]byte)
	dest.SetBytes(p)
	return nil
}

// NewInMemoryBackend get the singleton of groupcache
func NewInMemoryBackend(ctx context.Context, cacheKeyTemplate string) (Backend, error) {
	return &InMemoryBackend{
		Ctx:              ctx,
		Group:            groupch,
		CacheKeyTemplate: cacheKeyTemplate,
	}, nil
}

func (i *InMemoryBackend) Write(p []byte) (n int, err error) {
	i.Ctx = context.WithValue(i.Ctx, getterCtxKey, p)
	err = i.Group.Get(i.Ctx, i.CacheKeyTemplate, groupcache.AllocatingByteSliceSink(&i.cachedBytes))
	return len(i.cachedBytes), err
}

func (i *InMemoryBackend) Flush() error {
	return nil
}

func (i *InMemoryBackend) Clean() error {
	return nil
}

func (i *InMemoryBackend) Close() error {
	return nil
}

func (i *InMemoryBackend) GetReader() (io.ReadCloser, error) {
	rdr := ioutil.NopCloser(bytes.NewReader(i.cachedBytes))
	return rdr, nil
}
