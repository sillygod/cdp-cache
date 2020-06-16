package backends

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/golang/groupcache"
	"github.com/sillygod/cdp-cache/pkg/helper"
)

type ctxKey string

const getterCtxKey ctxKey = "getter"

var (
	groupName = "http_cache"
	groupch   *groupcache.Group
	pool      *groupcache.HTTPPool
	l         sync.Mutex
	srv       *http.Server
)

// InMemoryBackend saves the content into inmemory with the groupcache.
type InMemoryBackend struct {
	Ctx         context.Context
	Key         string
	content     bytes.Buffer
	cachedBytes []byte
}

func GetGroupCachePool() *groupcache.HTTPPool {
	return pool
}

func ReleaseGroupCacheRes() error {
	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			return err
		}
	}
	return nil
}

// InitGroupCacheRes init the resources for groupcache
// init this in the handler provision stage.
func InitGroupCacheRes(maxSize int) error {
	var err error

	l.Lock()
	defer l.Unlock()

	poolOptions := &groupcache.HTTPPoolOptions{}

	ip, err := helper.IPAddr()
	if err != nil {
		return err
	}

	self := "http://" + ip.String()
	if pool == nil {
		pool = groupcache.NewHTTPPoolOpts(self, poolOptions)
	}

	if groupch == nil {
		groupch = groupcache.NewGroup(groupName, int64(maxSize), groupcache.GetterFunc(getter))
	}

	mux := http.NewServeMux()
	mux.Handle("/_groupcache/", pool)
	srv = &http.Server{
		Addr:    ":http",
		Handler: mux,
	}

	errChan := make(chan error, 1)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	errChan <- nil

	return <-errChan
}

func getter(ctx context.Context, key string, dest groupcache.Sink) error {
	p, ok := ctx.Value(getterCtxKey).([]byte)
	if !ok {
		return errors.New("no precollcect content")
	}

	if err := dest.SetBytes(p); err != nil {
		return err
	}

	return nil
}

// NewInMemoryBackend get the singleton of groupcache
func NewInMemoryBackend(ctx context.Context, key string, expiration time.Time) (Backend, error) {
	// add the expiration time as the suffix of the key
	i := &InMemoryBackend{Ctx: ctx}
	// i.Key = i.composeKey(key, expiration)
	i.Key = key
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

// Flush do nothing here
func (i *InMemoryBackend) Flush() error {
	return nil
}

// Clean performs the purge storage
func (i *InMemoryBackend) Clean() error {
	// NOTE: there is no way to del or update the cache in groupcache
	// Therefore, I use the cache invalidation instead.
	return nil
}

// Close writeh the temp buffer's content to the groupcache
func (i *InMemoryBackend) Close() error {
	i.Ctx = context.WithValue(i.Ctx, getterCtxKey, i.content.Bytes())

	err := groupch.Get(i.Ctx, i.Key, groupcache.AllocatingByteSliceSink(&i.cachedBytes))
	if err != nil {
		caddy.Log().Named("backend:memory").Error(err.Error())
	}

	return err
}

// GetReader return a reader for the write public response
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
