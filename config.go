package cambridge_lookup_api

import (
	"errors"
	"sync"
	"time"

	"appengine"
	"appengine/datastore"
)

var config map[string]string
var configSync sync.Mutex
var configSetup sync.Once

type Config struct {
	Value       string
	LastUpdated time.Time
}

func getConfig(ctx appengine.Context, key string) (string, error) {
	configSync.Lock()
	defer configSync.Unlock()

	configSetup.Do(func() {
		config = make(map[string]string)
	})

	// grab the config
	val, ok := config[key]

	if ok {
		return val, nil
	}

	// not in memory - query database

	conf := new(Config)
	k := datastore.NewKey(ctx, "Config", key, 0, nil)

	if err := datastore.Get(ctx, k, conf); err != nil {
		return "", errors.New("Config does not exist")
	}

	config[key] = conf.Value

	return conf.Value, nil
}

func setConfig(ctx appengine.Context, key string, val string) error {
	k := datastore.NewKey(ctx, "Config", key, 0, nil)
	conf := Config{
		Value:       val,
		LastUpdated: time.Now(),
	}
	_, err := datastore.Put(ctx, k, &conf)
	return err
}
