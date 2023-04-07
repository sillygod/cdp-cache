package mystorage

import (
	"context"
	"fmt"
	"log"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/ory/dockertest/v3"
	"github.com/stretchr/testify/suite"
)

type StorageConsulTestSuite struct {
	suite.Suite
	pool     *dockertest.Pool
	resource *dockertest.Resource
	sg       *Storage
}

func (suite *StorageConsulTestSuite) initConsulSg(port string) error {
	h := httpcaddyfile.Helper{
		Dispenser: caddyfile.NewTestDispenser(fmt.Sprintf(`
		{
			storage consul {
				addr "localhost:%s"
				key_prefix "caddy_https"
			}
		}
		`, port)),
	}
	suite.sg = new(Storage)

	if err := suite.sg.UnmarshalCaddyfile(h.Dispenser); err != nil {
		return err
	}

	if err := suite.sg.Provision(caddy.Context{}); err != nil {
		return err
	}

	return nil
}

func (suite *StorageConsulTestSuite) SetupSuite() {
	pool, err := dockertest.NewPool("")
	if err != nil {
		log.Fatal(err)
	}

	consulVersion := "1.9.5"
	resource, err := pool.Run("consul", consulVersion, []string{})
	if err != nil {
		log.Fatal(err)
	}

	suite.pool = pool
	suite.resource = resource
	port := resource.GetPort("8500/tcp")

	suite.pool.Retry(func() error {
		return suite.initConsulSg(port)
	})

}

func (suite *StorageConsulTestSuite) TearDownSuite() {
	if err := suite.pool.Purge(suite.resource); err != nil {
		log.Fatal(err)
	}
}

func (suite *StorageConsulTestSuite) TestStore() {

	testData := []string{"hi", "hi/people"}
	ctx := context.Background()

	for _, data := range testData {
		err := suite.sg.Store(ctx, data, []byte(`OOOK`))
		suite.Nil(err)
	}

	res, err := suite.sg.List(ctx, "hi", true)
	suite.Nil(err)

	expectedRes := []string{}
	for _, data := range testData {
		key := suite.sg.generateKey(data)
		expectedRes = append(expectedRes, key)
	}

	suite.Equal(expectedRes, res)

}

func (suite *StorageConsulTestSuite) TestLoad() {
	ctx := context.Background()
	err := suite.sg.Store(ctx, "hi", []byte(`OOOK`))
	suite.Nil(err)
	value, err := suite.sg.Load(ctx, "hi")
	suite.Nil(err)

	suite.Equal([]byte(`OOOK`), value)
}

func (suite *StorageConsulTestSuite) TestDelete() {
	ctx := context.Background()
	err := suite.sg.Store(ctx, "hi", []byte(`OOOK`))
	suite.Nil(err)
	err = suite.sg.Delete(ctx, "hi")
	suite.Nil(err)
	exists := suite.sg.Exists(ctx, "hi")
	suite.False(exists)
}

func (suite *StorageConsulTestSuite) TestStat() {
	ctx := context.Background()
	err := suite.sg.Store(ctx, "hi", []byte(`OOOK`))
	suite.Nil(err)
	info, err := suite.sg.Stat(ctx, "hi")
	suite.Nil(err)
	suite.Equal("hi", info.Key)
}

func (suite *StorageConsulTestSuite) TestList() {
	ctx := context.Background()
	err := suite.sg.Store(ctx, "example.com", []byte(`OOOK`))
	suite.Nil(err)

	err = suite.sg.Store(ctx, "example.com/xx.crt", []byte(`OOOK`))
	suite.Nil(err)

	err = suite.sg.Store(ctx, "example.com/xx.csr", []byte(`OOOK`))
	suite.Nil(err)

	keys, err := suite.sg.List(ctx, "example.com", true)
	suite.Nil(err)
	suite.Len(keys, 3)
}

func (suite *StorageConsulTestSuite) TestLockUnlock() {
	ctx := context.Background()
	err := suite.sg.Lock(ctx, "example.com/lock")
	suite.Nil(err)
	err = suite.sg.Unlock(ctx, "example.com/lock")
	suite.Nil(err)
}

func (suite *StorageConsulTestSuite) TestExist() {
	ctx := context.Background()
	err := suite.sg.Store(ctx, "hi", []byte(`OOOK`))
	suite.Nil(err)
	exists := suite.sg.Exists(ctx, "hi")
	suite.True(exists)
}

func TestStorageConsulTestSuite(t *testing.T) {
	suite.Run(t, new(StorageConsulTestSuite))
}
