# Distributed WebSocket

Distributed WebSocket 是一个支持分布式部署的 WebSocket 服务框架，基于 Go 语言实现。它通过 Redis 存储客户端连接信息，利用 gRPC 在服务节点间传递消息，实现了跨节点的实时通信。

## 特性

- 支持单节点和分布式部署
- 基于 Redis 的连接信息共享
- 通过 gRPC 实现跨节点消息传递
- 高性能的 WebSocket 连接处理
- 自动连接管理和消息广播
- 客户端连接状态监控和错误处理

## 架构

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│  WebSocket 客户端 │────▶│ WebSocket 服务节点 │◀───▶│     Redis       │
└─────────────────┘     └──────────────────┘     └─────────────────┘
                               │      ▲
                               ▼      │
                        ┌──────────────────┐
                        │     gRPC         │
                        │   服务接口        │
                        └──────────────────┘
```

系统主要组件：

1. **WebSocket 客户端** - 浏览器或其他设备建立 WebSocket 连接
2. **WebSocket 服务节点** - 承载 WebSocket 连接，处理消息收发
3. **Redis** - 存储连接的元信息（connection_id ↔ 节点IP+端口）
4. **gRPC 服务** - 节点间通过 gRPC 互相发送消息

## 安装

```bash
go get github.com/jayecc/go-websocket
```

## 依赖

- Go 1.19+
- Redis
- gRPC

## 快速开始

### 单节点模式

```go
package main

import (
    "log"
    "net/http"
    "time"
    
    "github.com/gin-gonic/gin"
    websocket "github.com/jayecc/go-websocket"
)

func main() {
    gin.SetMode(gin.ReleaseMode)
    gin.DisableConsoleColor()
    app := gin.Default()

    // 创建WebSocket Hub
    hub := websocket.NewHubRun()
    defer hub.Close()

    // 注册WebSocket路由
    app.GET("/ws", func(ctx *gin.Context) {
        client := websocket.NewClient(hub)

        // 设置连接回调
        client.OnConnect(func(conn *websocket.Client) {
            log.Printf("Client %s connected", conn.GetID())
        })

        // 设置消息处理回调
        client.OnEvent(func(conn *websocket.Client, messageType int, message []byte) {
            log.Printf("Received message from client %s: %s", conn.GetID(), string(message))
            // 发送服务器时间作为响应
            response := time.Now().Format(time.RFC3339)
            conn.Emit([]byte(response))
        })

        // 设置断开连接回调
        client.OnDisconnect(func(id string) {
            log.Printf("Client %s disconnected", id)
        })

        // 设置错误处理回调
        client.OnError(func(id string, err error) {
            log.Printf("Error from client %s: %v", id, err)
        })

        // 建立WebSocket连接
        if err := client.Conn(ctx.Writer, ctx.Request); err != nil {
            log.Printf("Failed to establish WebSocket connection: %v", err)
            ctx.String(http.StatusInternalServerError, "Failed to establish WebSocket connection")
            return
        }
    })

    log.Fatal(app.Run(":8080"))
}
```

### 分布式模式

```go
package main

import (
    "context"
    "log"
    "net"
    "net/http"
    
    "github.com/gin-gonic/gin"
    "github.com/go-redis/redis/v8"
    "github.com/grpc-ecosystem/go-grpc-middleware/recovery"
    "golang.org/x/sync/errgroup"
    "google.golang.org/grpc"
    
    websocket "github.com/jayecc/go-websocket"
    "github.com/jayecc/go-websocket/websocketpb"
)

func main() {
    serverGroup := errgroup.Group{}
    grpcAddr := ":8081"
    httpAddr := ":8082"
    grpcHost := websocket.IP().String() + grpcAddr
    
    // 创建Redis客户端
    redisClient := redis.NewClient(&redis.Options{
        Addr: "localhost:6379", 
        Password: "", 
        DB: 0,
    })
    
    // 创建Redis存储
    storage := websocket.NewRedisStorage(redisClient, "websocket")
    
    // 创建WebSocket Hub
    websocketHub := websocket.NewHubRun()
    defer websocketHub.Close()
    
    // 创建分布式WebSocket客户端
    websocketClient := websocket.NewDistClient(storage)

    // 启动 gRPC 服务器
    serverGroup.Go(func() error {
        lis, err := net.Listen("tcp", grpcAddr)
        if err != nil {
            return err
        }
        
        grpcServer := grpc.NewServer(
            grpc.UnaryInterceptor(grpcrecovery.UnaryServerInterceptor()),
        )
        
        // 注册 gRPC 服务
        websocketpb.RegisterWebsocketServer(grpcServer, websocket.NewDistServer(websocketHub))
        return grpcServer.Serve(lis)
    })

    // 启动 HTTP 服务器
    serverGroup.Go(func() error {
        gin.SetMode(gin.ReleaseMode)
        gin.DisableConsoleColor()
        app := gin.Default()
        
        // 注册WebSocket路由
        app.GET("/ws", func(ctx *gin.Context) {
            session := websocket.NewDistSession(websocketHub, storage, grpcHost)
            
            session.OnError(func(id string, err error) {
                log.Printf("OnError: %v\n", err)
            })
            
            session.OnEvent(func(conn *websocket.Client, messageType int, message []byte) {
                log.Printf("OnEvent: %s\n", string(message))
                // 广播消息到所有节点
                log.Println(websocketClient.Broadcast(context.Background(), []byte("grpc广播消息")))
            })
            
            session.OnConnect(func(conn *websocket.Client) {
                log.Printf("OnConnect: %s\n", conn.GetID())
            })
            
            session.OnDisconnect(func(id string) {
                log.Printf("OnDisconnect: %s\n", id)
            })
            
            if err := session.Conn(ctx.Writer, ctx.Request); err != nil {
                ctx.String(http.StatusInternalServerError, "Failed to establish WebSocket connection")
                return
            }
        })
        
        return app.Run(httpAddr)
    })
    
    log.Println(serverGroup.Wait())
}
```

## API 参考

### 核心类型

#### Hub
管理活跃的客户端连接和消息广播。

- `NewHub()` - 创建 Hub 实例
- `NewHubRun()` - 创建并运行 Hub 实例
- `Client(id string)` - 根据 ID 获取客户端
- `Broadcast(message []byte)` - 广播消息
- `Close()` - 关闭 Hub

#### Client
表示单个 WebSocket 客户端连接。

- `NewClient(hub *Hub, opts ...Option)` - 创建客户端
- `Conn(w http.ResponseWriter, r *http.Request)` - 建立 WebSocket 连接
- `Emit(message []byte)` - 向客户端发送消息
- `Broadcast(message []byte)` - 广播消息
- `GetID()` - 获取客户端 ID
- `Close()` - 关闭客户端连接

#### DistSession
分布式会话管理器。

- `NewDistSession(hub *Hub, storage Storage, addr string, opts ...Option)` - 创建分布式会话
- `OnConnect(handler func(conn *Client))` - 设置连接回调
- `OnEvent(handler func(conn *Client, messageType int, message []byte))` - 设置消息回调
- `OnError(handler func(id string, err error))` - 设置错误回调
- `OnDisconnect(handler func(id string))` - 设置断开连接回调

#### DistClient
分布式客户端，用于跨节点发送消息。

- `NewDistClient(storage Storage)` - 创建分布式客户端
- `Emit(ctx context.Context, id string, message []byte)` - 向指定客户端发送消息
- `Online(ctx context.Context, id string)` - 检查客户端是否在线
- `Broadcast(ctx context.Context, message []byte)` - 广播消息到所有节点

#### Storage
存储接口，用于保存客户端连接信息。

- `Set(key string, value string)` - 设置键值对
- `Get(key string)` - 获取值
- `Del(key ...string)` - 删除键
- `Clear(host string)` - 清理指定主机的连接
- `All()` - 获取所有连接信息

### gRPC 服务

项目提供了 gRPC 服务接口，用于节点间通信：

- `Emit` - 向指定客户端发送消息
- `Online` - 检查客户端是否在线
- `Broadcast` - 广播消息

## 配置

### Redis 配置

```go
redisClient := redis.NewClient(&redis.Options{
    Addr: "localhost:6379",
    Password: "", 
    DB: 0,
})
```

### 存储前缀

```go
storage := websocket.NewRedisStorage(redisClient, "websocket")
```

## 最佳实践

1. **错误处理**：始终实现错误回调以监控连接问题
2. **资源清理**：使用 `defer` 确保 Hub 和连接正确关闭
3. **超时控制**：为 gRPC 调用设置合适的超时时间
4. **重试机制**：对于关键操作实现重试逻辑
5. **监控日志**：记录连接状态和消息处理日志

## 许可证

MIT
