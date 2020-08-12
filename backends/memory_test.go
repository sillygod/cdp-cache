package backends

import (
	"context"
	"io/ioutil"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

type MemoryBackendTestSuite struct {
	suite.Suite
}

func (suite *MemoryBackendTestSuite) SetupSuite() {
	err := InitGroupCacheRes(50 * 1024 * 1024)
	suite.Nil(err)
}

func (suite *MemoryBackendTestSuite) TearDownSuite() {
	err := ReleaseGroupCacheRes()
	suite.Nil(err)
}

func (suite *MemoryBackendTestSuite) TestWriteInCache() {
	ctx := context.Background()
	backend, err := NewInMemoryBackend(ctx, "hello", time.Now())
	suite.Nil(err)

	content := []byte("hello world")
	length, err := backend.Write(content)
	suite.Nil(err)
	suite.Equal(len(content), length)

	// now start to write in the groupcache
	backend.Close()
	suite.Equal(len(content), backend.Length())

	// test the content get from the reader will be consistent with the original one
	reader, err := backend.GetReader()
	suite.Nil(err)
	result, err := ioutil.ReadAll(reader)
	suite.Nil(err)
	suite.Equal(result, content)
}

func (suite *MemoryBackendTestSuite) TestReadExistingCacheInGroupCache() {

	ctx := context.Background()
	backend, err := NewInMemoryBackend(ctx, "hello", time.Now().Add(1*time.Minute))
	suite.Assert().NoError(err)
	content := []byte("hello world")
	length, err := backend.Write(content)
	suite.Assert().NoError(err)
	suite.Equal(len(content), length)
	backend.Close()

	// new a InMemoryBackend with the same key and test getting cache content
	// this case will happen when caddy restart or other scenario.
	// The cache is in groupcache but the client doesn't has the correspond backend mapping
	anotherBackend, err := NewInMemoryBackend(context.Background(), "hello", time.Now().Add(1*time.Minute))
	suite.Assert().NoError(err)
	reader, err := anotherBackend.GetReader()
	suite.Assert().NoError(err)

	result, err := ioutil.ReadAll(reader)
	suite.Assert().NoError(err)
	suite.Equal(result, content)

}

func (suite *MemoryBackendTestSuite) TestCleanCache() {
	ctx := context.Background()
	backend, err := NewInMemoryBackend(ctx, "be_cleaned_key", time.Now().Add(1*time.Minute))
	suite.Assert().NoError(err)

	err = backend.Clean()
	suite.Assert().NoError(err)

	_, err = backend.GetReader()
	_, ok := err.(NoPreCollectError)
	suite.True(ok)
}

func TestMemoryBackendTestSuite(t *testing.T) {
	suite.Run(t, new(MemoryBackendTestSuite))
}
