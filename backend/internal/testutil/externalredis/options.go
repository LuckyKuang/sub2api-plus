// Package externalredis configures explicitly isolated integration-test clients.
package externalredis

import (
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"
)

// Options requires a nonzero test database when reusing an external Redis.
// Redis logical databases do not isolate subscription channels: suites that use
// Pub/Sub require a dedicated Redis process as well as test credentials.
func Options(address, databaseEnvironment string) (*redis.Options, error) {
	database, err := strconv.Atoi(os.Getenv(databaseEnvironment))
	if err != nil || database < 1 {
		return nil, errors.New(databaseEnvironment + " must select a dedicated nonzero Redis database")
	}
	if strings.TrimSpace(address) == "" {
		return nil, errors.New("external test Redis address is required")
	}
	return &redis.Options{
		Addr:     address,
		DB:       database,
		Username: os.Getenv("SUB2API_TEST_REDIS_USERNAME"),
		Password: os.Getenv("SUB2API_TEST_REDIS_PASSWORD"),
	}, nil
}
