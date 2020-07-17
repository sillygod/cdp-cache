package httpcache

import (
	"io"
	"io/ioutil"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/suite"
)

type TestBackend struct {
	recorder *httptest.ResponseRecorder
	closed   bool
	flushed  bool
	cleaned  bool
}

func NewTestBackend() *TestBackend {
	return &TestBackend{
		recorder: httptest.NewRecorder(),
	}
}

func (t *TestBackend) Length() int {
	return t.recorder.Body.Len()
}

func (t *TestBackend) Write(p []byte) (int, error) {
	return t.recorder.Write(p)
}

func (t *TestBackend) Flush() error {
	t.flushed = true
	return nil
}

func (t *TestBackend) Clean() error {
	t.cleaned = true
	return nil
}

func (t *TestBackend) Close() error {
	t.closed = true
	return nil
}

func (t *TestBackend) GetReader() (io.ReadCloser, error) {
	return t.recorder.Result().Body, nil
}

type ResponseTestSuite struct {
	suite.Suite
}

func (suite *ResponseTestSuite) TestResponseSendHeaders() {
	r := NewResponse()
	go func() {
		r.Header().Add("Content-Type", "application/json")
		r.WriteHeader(200)
	}()

	r.WaitHeaders()
	suite.Equal(r.Header().Get("Content-Type"), "application/json")
}

func (suite *ResponseTestSuite) TestResponseWaitBackend() {
	r := NewResponse()
	requestEntered := make(chan struct{}, 1)
	writtenChan := make(chan struct{}, 1)
	originalContent := []byte("hello")

	go func() {
		requestEntered <- struct{}{}
		r.Write(originalContent)
		writtenChan <- struct{}{}
	}()

	r.WaitHeaders()
	<-requestEntered
	suite.Len(writtenChan, 0)

	backend := NewTestBackend()
	r.SetBody(backend)
	<-writtenChan

	r.Close()
	reader, err := r.GetReader()
	if err != nil {
		suite.Error(err)
	}

	content, err := ioutil.ReadAll(reader)
	if err != nil {
		suite.Error(err)
	}
	suite.Equal(originalContent, content)
}

func (suite *ResponseTestSuite) TestCloseResponse() {
	r := NewResponse()

	go func() {
		r.WriteHeader(200)
	}()

	r.WaitHeaders()
	backend := NewTestBackend()
	r.SetBody(backend)
	r.Close()
	r.Flush()

	suite.True(backend.closed)
}

func (suite *ResponseTestSuite) TestCleanResponse() {
	r := NewResponse()
	backend := NewTestBackend()
	r.SetBody(backend)
	r.Close()
	r.Clean()
	suite.True(backend.cleaned)
}

func (suite *ResponseTestSuite) TestSetNilBody() {
	r := NewResponse()
	r.SetBody(nil)
	n, err := r.Write([]byte(`hello`))
	suite.Equal(0, n)
	suite.Error(err)
}

func TestResponseTestSuite(t *testing.T) {
	suite.Run(t, new(ResponseTestSuite))
}
