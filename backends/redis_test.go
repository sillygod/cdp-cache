package backends

import (
	"context"
	"fmt"
	"io"
	"log"
	"testing"
	"time"

	"github.com/ory/dockertest/v3"
	"github.com/stretchr/testify/suite"
)

type RedisBackendTestSuite struct {
	suite.Suite
	pool     *dockertest.Pool
	resource *dockertest.Resource
}

func (suite *RedisBackendTestSuite) SetupSuite() {
	pool, err := dockertest.NewPool("")
	if err != nil {
		log.Fatal(err)
	}

	redisVersion := "5.0.9"
	resource, err := pool.Run("redis", redisVersion, []string{})
	if err != nil {
		log.Fatal(err)
	}

	suite.pool = pool
	suite.resource = resource
	port := resource.GetPort("6379/tcp")

	err = suite.pool.Retry(func() error {
		return InitRedisClient(fmt.Sprintf("localhost:%s", port), "", 0)
	})
	if err != nil {
		log.Fatal(err)
	}
}

func (suite *RedisBackendTestSuite) TestParseRedisConfig() {
	opts, err := ParseRedisConfig("localhost:6379 1 songa")
	suite.Nil(err)
	opts.Addr = "localhost:6379"
	opts.DB = 1
	opts.Password = "songa"
}

func (suite *RedisBackendTestSuite) TestWriteCacheInRedis() {
	backend, err := NewRedisBackend(context.Background(), "hello", time.Now().Add(5*time.Minute))
	suite.Nil(err)
	content := []byte("hello world")
	backend.Write(content)

	suite.Equal(len(content), backend.Length())

	err = backend.Close()
	suite.Nil(err)

	reader, err := backend.GetReader()
	suite.Nil(err)
	result, err := io.ReadAll(reader)
	suite.Nil(err)
	suite.Equal(content, result)
}

func (suite *RedisBackendTestSuite) TearDownSuite() {
	if err := suite.pool.Purge(suite.resource); err != nil {
		log.Fatal(err)
	}
}

func TestRedisBackendTestSuite(t *testing.T) {
	suite.Run(t, new(RedisBackendTestSuite))
}
