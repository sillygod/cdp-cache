package backends

import (
	"io"
	"io/ioutil"
	"os"
	"sync"
)

// FileBackend saves the content into a file
type FileBackend struct {
	file         *os.File
	subscription *Subscription
}

// NewFileBackend new a disk storage backend
func NewFileBackend(path string) (Backend, error) {
	err := os.MkdirAll(path, os.ModePerm)
	if err != nil {
		return nil, err
	}

	file, err := ioutil.TempFile(path, "caddy-cache-")
	if err != nil {
		return nil, err
	}

	return &FileBackend{
		file:         file,
		subscription: NewSubscription(),
	}, nil
}

func (f *FileBackend) Write(p []byte) (n int, err error) {
	defer f.subscription.NotifyAll(len(p))
	return f.file.Write(p)
}

// Flush syncs the underlying file
func (f *FileBackend) Flush() error {
	defer f.subscription.NotifyAll(0)
	return f.file.Sync()
}

func (f *FileBackend) Clean() error {
	f.subscription.WaitAll()
	return os.Remove(f.file.Name())
}

func (f *FileBackend) Close() error {
	f.subscription.Close()
	return f.file.Close()
}

func (f *FileBackend) GetReader() (io.ReadCloser, error) {
	newFile, err := os.Open(f.file.Name())
	if err != nil {
		return nil, err
	}

	return &FileReader{
		content:      newFile,
		subscription: f.subscription.NewSubscriber(),
		unsubscribe:  f.subscription.RemoveSubscriber,
	}, nil
}

// FileReader is the common code to read the storages until the subscription channel is closed
type FileReader struct {
	subscription <-chan int
	content      io.ReadCloser
	unsubscribe  func(<-chan int)
}

func (r *FileReader) Read(p []byte) (n int, err error) {
	for range r.subscription {
		n, err := r.content.Read(p)
		if err != io.EOF {
			return n, err
		}
	}

	// if there is no subscription, just read
	return r.content.Read(p)
}

// Close closes the underlying storage
func (r *FileReader) Close() error {
	err := r.content.Close()
	r.unsubscribe(r.subscription)
	return err
}

// Subscription ..
type Subscription struct {
	closed           bool
	closedLock       *sync.RWMutex
	subscribers      []chan int
	noSubscriberChan chan struct{}
	subscribersLock  *sync.RWMutex
}

func NewSubscription() *Subscription {
	return &Subscription{
		closedLock:       new(sync.RWMutex),
		subscribersLock:  new(sync.RWMutex),
		noSubscriberChan: make(chan struct{}, 1),
	}
}

func (s *Subscription) NewSubscriber() <-chan int {
	s.closedLock.Lock()
	defer s.closedLock.Unlock()

	if s.closed {
		subscription := make(chan int)
		close(subscription)
		return subscription
	}

	s.subscribersLock.Lock()
	defer s.subscribersLock.Unlock()
	subscription := make(chan int, 1)
	s.subscribers = append(s.subscribers, subscription)
	return subscription
}

func (s *Subscription) RemoveSubscriber(subscriber <-chan int) {
	s.subscribersLock.Lock()
	defer s.subscribersLock.Unlock()

	for i, x := range s.subscribers {
		if x == subscriber {
			s.subscribers = append(s.subscribers[:i], s.subscribers[i+1:]...)
		}
	}

	noSubscribers := len(s.subscribers) == 0

	if noSubscribers {
		select {
		case s.noSubscriberChan <- struct{}{}:
		default:
		}
	}
}

func (s *Subscription) Close() {
	s.closedLock.Lock()
	defer s.closedLock.Unlock()

	if s.closed {
		return
	}

	s.closed = true
	s.subscribersLock.RLock()
	defer s.subscribersLock.RUnlock()

	// close all channel
	for _, subscriber := range s.subscribers {
		close(subscriber)
	}
}

func (s *Subscription) NotifyAll(newBytes int) {
	s.subscribersLock.RLock()
	defer s.subscribersLock.RUnlock()

	for _, subscriber := range s.subscribers {
		select {
		case subscriber <- newBytes:
		default:
		}
	}
}

func (s *Subscription) hasSubscribers() bool {
	s.subscribersLock.RLock()
	defer s.subscribersLock.RUnlock()
	return len(s.subscribers) != 0
}

func (s *Subscription) WaitAll() {
	if !s.hasSubscribers() {
		return
	}

	<-s.noSubscriberChan
}
