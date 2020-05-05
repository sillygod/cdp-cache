package backends

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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