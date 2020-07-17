package backends

import (
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type FileBackendTestSuite struct {
	suite.Suite
}

func (suite *FileBackendTestSuite) generateRandomPath(length int) string {
	letters := "abcedfghijklmnoprqstuvwxyz"
	result := make([]byte, length)
	for i := range result {
		result[i] += letters[rand.Intn(len(letters))]
	}

	return string(result)
}

func (suite *FileBackendTestSuite) TestReadAfterWrite() {

	backend, err := NewFileBackend("/tmp/test")
	suite.Nil(err)
	defer backend.Close()

	reader, _ := backend.GetReader()
	defer reader.Close()

	content := []byte("hello world")
	backend.Write(content)

	buf := make([]byte, len(content))
	_, err = reader.Read(buf)
	suite.Nil(err)

	suite.Equal(content, buf)
}

func (suite *FileBackendTestSuite) TestAutoCreateDirIfNonExist() {
	path := suite.generateRandomPath(5)
	dirName := filepath.Join("/tmp", path)

	_, err := os.Stat(dirName)
	suite.True(os.IsNotExist(err))
	backend, err := NewFileBackend(dirName)
	suite.Nil(err)
	_, err = os.Stat(dirName)
	suite.Nil(err)
	backend.Close()
}

func (suite *FileBackendTestSuite) TestMultiClose() {
	backend, err := NewFileBackend("/tmp/hello")
	suite.Nil(err)
	backend.Close()
	backend.Close()

}

func (suite *FileBackendTestSuite) TestLengthShouldBeZero() {
	backend, err := NewFileBackend("/tmp/hello")
	suite.Nil(err)
	n := backend.Length()
	suite.Equal(0, n)
}

func (suite *FileBackendTestSuite) TestDeleteFileAfterCleaned() {
	backend, err := NewFileBackend("/tmp/hello")
	suite.Nil(err)

	fileName := backend.(*FileBackend).file.Name()
	_, err = os.Stat(fileName)
	suite.Nil(err)

	backend.Close()
	backend.Clean()

	_, err = os.Stat(fileName)
	suite.True(os.IsNotExist(err))
}

func TestSubscription(t *testing.T) {

	t.Run("notify all", func(t *testing.T) {
		s := NewSubscription()

		s1 := s.NewSubscriber()
		s2 := s.NewSubscriber()
		s.NotifyAll(10)

		c1 := <-s1
		require.Equal(t, c1, 10)
		c2 := <-s2
		require.Equal(t, c2, 10)

		s.NotifyAll(5)

		require.Len(t, s1, 1)
		require.Len(t, s2, 1)
	})

	t.Run("should wait until all subscribers unsubscribe to continue", func(t *testing.T) {
		s := NewSubscription()

		s1 := s.NewSubscriber()
		s2 := s.NewSubscriber()

		s.NotifyAll(9)

		waitCalled := make(chan struct{}, 1)
		ended := make(chan struct{}, 1)

		go func() {
			waitCalled <- struct{}{}
			s.WaitAll()
			ended <- struct{}{}
		}()

		require.Len(t, ended, 0)
		<-waitCalled
		s.RemoveSubscriber(s1)
		require.Len(t, ended, 0)
		s.RemoveSubscriber(s2)
		<-ended
	})

}

func TestFileBackendTestSuite(t *testing.T) {
	suite.Run(t, new(FileBackendTestSuite))
}
