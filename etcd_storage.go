package websocket

import (
	"context"
	clientv3 "go.etcd.io/etcd/client/v3"
	"time"
)

// NewEtcdClient creates a new etcd client with the specified endpoints.
// It initializes the client with a dial timeout of 5 seconds.
//
// Parameters:
//   - endpoints: A slice of strings representing the etcd server addresses
//
// Returns:
//   - *clientv3.Client: A pointer to the created etcd client
//   - error: An error if client creation fails, nil otherwise
func NewEtcdClient(endpoints []string) (*clientv3.Client, error) {
	// Create a new etcd client with the provided configuration
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	return cli, nil
}

// EtcdStorage 是一个基于 etcd 的存储实现，用于操作带前缀的键值数据。
type EtcdStorage struct {
	client *clientv3.Client // etcd 客户端实例
	prefix string           // 所有键操作时使用的公共前缀
}

// NewEtcdStorage 创建一个新的 EtcdStorage 实例。
// 参数:
//   - client: 已初始化的 etcd v3 客户端
//   - prefix: 键名前缀，所有操作都会在这个前缀下进行
//
// 返回值:
//   - *EtcdStorage: 新创建的 EtcdStorage 实例
func NewEtcdStorage(client *clientv3.Client, prefix string) *EtcdStorage {
	return &EtcdStorage{
		client: client,
		prefix: prefix,
	}
}

// contextTimeout 创建一个具有超时控制的上下文。
// 默认超时时间为 2 秒。
// 返回值:
//   - context.Context: 带有超时的上下文
//   - context.CancelFunc: 取消该上下文的函数
func (s *EtcdStorage) contextTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 2*time.Second)
}

// Set 将指定的键值对存入 etcd 中。
// 参数:
//   - key: 不带前缀的键名
//   - value: 要保存的字符串值
//
// 返回值:
//   - error: 操作过程中发生的错误（如果有）
func (s *EtcdStorage) Set(key string, value string) error {
	ctx, cancel := s.contextTimeout()
	defer cancel()
	_, err := s.client.Put(ctx, s.prefix+key, value)
	return err
}

// Get 获取指定键对应的值。
// 参数:
//   - key: 不带前缀的键名
//
// 返回值:
//   - string: 对应的值；如果键不存在则返回空字符串
//   - error: 操作过程中发生的错误（如果有）
func (s *EtcdStorage) Get(key string) (string, error) {
	ctx, cancel := s.contextTimeout()
	resp, err := s.client.Get(ctx, s.prefix+key)
	cancel()
	if err != nil {
		return "", err
	}
	if len(resp.Kvs) == 0 {
		return "", nil // key 不存在返回空字符串
	}
	return string(resp.Kvs[0].Value), nil
}

// Del 删除一个或多个键。
// 参数:
//   - keys: 需要删除的一个或多个不带前缀的键名
//
// 返回值:
//   - error: 操作过程中发生的错误（如果有）
func (s *EtcdStorage) Del(keys ...string) error {
	ctx, cancel := s.contextTimeout()
	defer cancel()
	for _, key := range keys {
		_, err := s.client.Delete(ctx, s.prefix+key)
		if err != nil {
			return err
		}
	}
	return nil
}

// Clear 根据主机标识清理其下的所有相关键。
// 参数:
//   - host: 主机标识符，将删除以 "/prefix/host/" 开头的所有键
//
// 返回值:
//   - error: 操作过程中发生的错误（如果有）
func (s *EtcdStorage) Clear(host string) error {
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
	ctx, cancel := s.contextTimeout()
	defer cancel()
	for _, key := range remove {
		if _, err = s.client.Delete(ctx, s.prefix+key); err != nil {
			return err
		}
	}
	return nil
}

// All 获取当前前缀下的所有键值对。
// 返回值:
//   - map[string]string: 包含所有键值对的映射表
//   - error: 操作过程中发生的错误（如果有）
func (s *EtcdStorage) All() (map[string]string, error) {
	result := make(map[string]string)
	ctx, cancel := s.contextTimeout()
	resp, err := s.client.Get(ctx, s.prefix, clientv3.WithPrefix())
	cancel()
	if err != nil {
		return nil, err
	}
	// 遍历响应中的键值对并填充到结果中
	for _, kv := range resp.Kvs {
		result[string(kv.Key)] = string(kv.Value)
	}
	return result, nil
}
