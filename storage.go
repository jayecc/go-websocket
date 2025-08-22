package websocket

import (
	"context"
	"errors"
	"github.com/go-redis/redis/v8"
	"time"
)

// Storage defines the interface for storing and retrieving WebSocket client information.
// It is used in distributed WebSocket setups to share client location information.
type Storage interface {
	// Set stores a key-value pair in the storage.
	// In distributed WebSocket, this typically stores client ID to server address mappings.
	Set(key string, value string) error

	// Get retrieves the value for a key from the storage.
	// Returns an empty string if the key does not exist.
	Get(key string) (string, error)

	// Del removes one or more keys from the storage.
	// It should not return an error if a key does not exist.
	Del(key ...string) error

	// Clear removes all entries for a specific host from the storage.
	// This is used to clean up entries when a server shuts down.
	Clear(host string) error

	// All retrieves all key-value pairs from the storage.
	// This is used for maintenance and monitoring purposes.
	All() (map[string]string, error)
}

// RedisStorage implements the Storage interface using Redis.
// It uses a Redis hash to store key-value pairs under a specified prefix.
type RedisStorage struct {
	// client is the Redis client used to perform operations
	client *redis.Client

	// prefix is the Redis key prefix for the hash used to store data
	prefix string
}

// NewRedisStorage creates a new RedisStorage instance with the given Redis client and key prefix.
func NewRedisStorage(redisClient *redis.Client, prefix string) *RedisStorage {
	return &RedisStorage{
		client: redisClient,
		prefix: prefix,
	}
}

// Set stores a key-value pair in Redis with a 2-second timeout.
// It uses HSet to store the data in a Redis hash.
func (s *RedisStorage) Set(key string, value string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.client.HSet(ctx, s.prefix, key, value).Err()
}

// Get retrieves the value for a key from Redis with a 2-second timeout.
// It uses HGet to retrieve data from a Redis hash.
// Returns an empty string and no error if the key does not exist.
func (s *RedisStorage) Get(key string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	value, err := s.client.HGet(ctx, s.prefix, key).Result()
	cancel()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	return value, err
}

// Del removes one or more keys from Redis with a 2-second timeout.
// It uses HDel to remove fields from a Redis hash.
// It does not return an error if a key does not exist.
func (s *RedisStorage) Del(key ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.client.HDel(ctx, s.prefix, key...).Err()
}

// Clear removes all entries for a specific host from Redis with a 2-second timeout.
// It first retrieves all entries, identifies those matching the host, and removes them.
// This is used to clean up when a server node shuts down.
func (s *RedisStorage) Clear(host string) error {
	all, err := s.All()
	if err != nil {
		return err
	}
	remove := make([]string, 0)
	for id, addr := range all {
		if addr != host {
			continue
		}
		remove = append(remove, id)
	}
	if len(remove) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.client.HDel(ctx, s.prefix, remove...).Err()
}

// All retrieves all key-value pairs from Redis with a 2-second timeout.
// It uses HGetAll to retrieve all fields and values from a Redis hash.
// Returns nil and no error if no entries exist.
func (s *RedisStorage) All() (map[string]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	values, err := s.client.HGetAll(ctx, s.prefix).Result()
	cancel()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	return values, err
}
