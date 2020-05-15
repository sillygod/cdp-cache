package httpcache

import (
	"hash/crc32"
	"math"
	"sync"
)

type URLLock struct {
	globalLocks        []*sync.Mutex
	keys               []map[string]*sync.Mutex
	urlLockBucketsSize int
}

func NewURLLock(config *Config) *URLLock {
	globalLocks := make([]*sync.Mutex, config.CacheBucketsNum)
	keys := make([]map[string]*sync.Mutex, config.CacheBucketsNum)

	for i := 0; i < config.CacheBucketsNum; i++ {
		globalLocks[i] = new(sync.Mutex)
		keys[i] = make(map[string]*sync.Mutex)
	}

	return &URLLock{
		globalLocks:        globalLocks,
		keys:               keys,
		urlLockBucketsSize: config.CacheBucketsNum,
	}
}

// Acquire a lock for given key
func (allLocks *URLLock) Acquire(key string) *sync.Mutex {
	bucketIndex := allLocks.getBucketIndexForKey(key)
	allLocks.globalLocks[bucketIndex].Lock()
	defer allLocks.globalLocks[bucketIndex].Unlock()

	lock, exists := allLocks.keys[bucketIndex][key]
	if !exists {
		lock = new(sync.Mutex)
		allLocks.keys[bucketIndex][key] = lock
	}
	lock.Lock()
	return lock
}

func (allLocks *URLLock) getBucketIndexForKey(key string) uint32 {
	return uint32(math.Mod(float64(crc32.ChecksumIEEE([]byte(key))), float64(allLocks.urlLockBucketsSize)))
}
