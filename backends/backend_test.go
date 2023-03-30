package backends

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/suite"
)

type BackendTestSuite struct {
	suite.Suite
	base *Base
}

func (b *BackendTestSuite) SetupSuite() {
	w := httptest.NewRecorder()
	b.base = &Base{w}
}

func (b *BackendTestSuite) TearDownSuite() {

}

func (b *BackendTestSuite) TestGetReader() {
	reader, err := b.base.GetReader()
	b.Nil(reader, "the reader should be nil")
	b.NotNil(err, "private response are not readable")
}

func (b *BackendTestSuite) TestLength() {
	b.Equal(0, b.base.Length(), "base backend no cache content so its length is zero")
}

func (b *BackendTestSuite) TestClean() {
	b.Nil(b.base.Clean())
}

func (b *BackendTestSuite) TestClose() {
	b.Nil(b.base.Close())
}

func (b *BackendTestSuite) TestWrite() {
	n, err := b.base.Write([]byte("hello"))
	b.Nil(err)
	b.Equal(5, n)
}

func (b *BackendTestSuite) TestFlush() {
	err := b.base.Flush()
	b.Nil(err)
}

func TestBackendTestSuite(t *testing.T) {
	suite.Run(t, new(BackendTestSuite))
}
