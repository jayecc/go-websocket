package websocket

import (
	"context"
	"errors"
	"github.com/go-redis/redis/v8"
	"time"
)

// NewRedisClient 创建一个新的Redis客户端实例
// 参数:
//
//	opt - Redis连接选项配置
//
// 返回值:
//
//	*redis.Client - Redis客户端实例指针
//	error - 连接错误信息，如果连接成功则为nil
func NewRedisClient(opt *redis.Options) (*redis.Client, error) {

	client := redis.NewClient(opt)

	// 验证Redis连接是否正常
	if _, err := client.Ping(context.Background()).Result(); err != nil {
		return nil, err
	}

	return client, nil
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

func (s *RedisStorage) contextTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 2*time.Second)
}

// Set stores a key-value pair in Redis with a 2-second timeout.
// It uses HSet to store the data in a Redis hash.
func (s *RedisStorage) Set(key string, value string) error {
	ctx, cancel := s.contextTimeout()
	defer cancel()
	return s.client.HSet(ctx, s.prefix, key, value).Err()
}

// Get retrieves the value for a key from Redis with a 2-second timeout.
// It uses HGet to retrieve data from a Redis hash.
// Returns an empty string and no error if the key does not exist.
func (s *RedisStorage) Get(key string) (string, error) {
	ctx, cancel := s.contextTimeout()
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
	ctx, cancel := s.contextTimeout()
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
	remove := make([]string, 0, len(all))
	for id, addr := range all {
		if addr != host {
			continue
		}
		remove = append(remove, id)
	}
	if len(remove) == 0 {
		return nil
	}
	ctx, cancel := s.contextTimeout()
	defer cancel()
	return s.client.HDel(ctx, s.prefix, remove...).Err()
}

// All retrieves all key-value pairs from Redis with a 2-second timeout.
// It uses HGetAll to retrieve all fields and values from a Redis hash.
// Returns nil and no error if no entries exist.
func (s *RedisStorage) All() (map[string]string, error) {
	ctx, cancel := s.contextTimeout()
	values, err := s.client.HGetAll(ctx, s.prefix).Result()
	cancel()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	return values, err
}
