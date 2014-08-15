package cambridge_lookup_api

import (
	"sync"
)

var cache map[string]string
var cacheSync sync.Mutex
var cacheSetup sync.Once

func getFromCache(key string) (string, bool) {
	cacheSync.Lock()
	defer cacheSync.Unlock()

	cacheSetup.Do(func() {
		cache = make(map[string]string, 1000)
	})

	val, err := cache[key]
	return val, err
}

func putInCache(key string, val string) {
	cacheSync.Lock()
	defer cacheSync.Unlock()

	cache[key] = val
}
