package backends

import (
	"bytes"
	"context"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	client *redis.Client
)

// RedisBackend saves the content into redis
type RedisBackend struct {
	Ctx        context.Context
	Key        string
	content    bytes.Buffer
	expiration time.Time
}

// ParseRedisConfig parses the connection settings string from the caddyfile
func ParseRedisConfig(connSetting string) (*redis.Options, error) {
	var err error
	args := strings.Split(connSetting, " ")
	addr, password, db := args[0], "", 0
	length := len(args)
	// the format of args: addr db password

	if length > 1 {
		db, err = strconv.Atoi(args[1])
		if err != nil {
			return nil, err
		}
	}

	if length > 2 {
		password = args[2]
	}

	return &redis.Options{
		Addr:     addr,
		DB:       db,
		Password: password,
	}, nil
}

// InitRedisClient inits the client for the redis
func InitRedisClient(addr, password string, db int) error {
	l.Lock()
	defer l.Unlock()

	client = redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	ctx := context.Background()

	if _, err := client.Ping(ctx).Result(); err != nil {
		return err
	}

	return nil
}

// NewRedisBackend new a redis backend for cache's storage
func NewRedisBackend(ctx context.Context, key string, expiration time.Time) (Backend, error) {
	return &RedisBackend{
		Ctx:        ctx,
		Key:        key,
		expiration: expiration,
	}, nil
}

// Write writes the response content in a temp buffer
func (r *RedisBackend) Write(p []byte) (n int, err error) {
	return r.content.Write(p)
}

// Flush do nothing here
func (r *RedisBackend) Flush() error {
	return nil
}

// Length return the cache content's length
func (r *RedisBackend) Length() int {
	return r.content.Len()
}

// Close write the temp buffer's content to the groupcache
func (r *RedisBackend) Close() error {
	_, err := client.Set(r.Ctx, r.Key, r.content.Bytes(), r.expiration.Sub(time.Now())).Result()
	return err
}

// Clean performs the purge storage
func (r *RedisBackend) Clean() error {
	_, err := client.Del(r.Ctx, r.Key).Result()
	return err
}

// GetReader return a reader for the write public response
func (r *RedisBackend) GetReader() (io.ReadCloser, error) {
	content, err := client.Get(r.Ctx, r.Key).Result()
	if err != nil {
		return nil, err
	}

	rc := io.NopCloser(strings.NewReader(content))
	return rc, nil
}
