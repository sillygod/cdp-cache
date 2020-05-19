package backends

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"sync"
	"time"

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

// InMemoryBackend saves the content into inmemory with the groupcache.
type InMemoryBackend struct {
	Ctx         context.Context
	Key         string
	content     bytes.Buffer
	cachedBytes []byte
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
		if err != nil {
			return err
		}
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
func NewInMemoryBackend(ctx context.Context, key string, expiration time.Time) (Backend, error) {
	// add the expiration time as the suffix of the key
	i := &InMemoryBackend{Ctx: ctx}
	i.Key = i.composeKey(key, expiration)
	return i, nil
}

func (i *InMemoryBackend) composeKey(key string, expiration time.Time) string {
	return fmt.Sprintf("%s:%d", key, expiration.Unix())
}

// Write adds the response content in the context for the groupcache's
// setter function.
func (i *InMemoryBackend) Write(p []byte) (n int, err error) {
	return i.content.Write(p)
}

func (i *InMemoryBackend) Flush() error {
	return nil
}

func (i *InMemoryBackend) Clean() error {
	// NOTE: there is no way to del or update the cache in groupcache
	// Therefore, I use the cache invalidation instead.
	return nil
}

func (i *InMemoryBackend) Close() error {
	i.Ctx = context.WithValue(i.Ctx, getterCtxKey, i.content.Bytes())
	err := groupch.Get(i.Ctx, i.Key, groupcache.AllocatingByteSliceSink(&i.cachedBytes))
	return err
}

func (i *InMemoryBackend) GetReader() (io.ReadCloser, error) {
	if len(i.cachedBytes) == 0 {
		err := groupch.Get(i.Ctx, i.Key, groupcache.AllocatingByteSliceSink(&i.cachedBytes))
		if err != nil {
			return nil, err
		}
	}

	rc := ioutil.NopCloser(bytes.NewReader(i.cachedBytes))
	return rc, nil
}
