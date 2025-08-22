package websocket

import (
	"context"
	"fmt"
	"github.com/jayecc/go-websocket/websocketpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"net/http"
	"sync"
)

// DistServer implements the gRPC server for distributed WebSocket operations.
// It handles requests from other nodes in the cluster to perform operations on locally connected clients.
type DistServer struct {
	// hub is the local hub containing clients connected to this server instance
	hub *Hub
}

// NewDistServer creates a new DistServer instance with the given hub.
func NewDistServer(hub *Hub) *DistServer {
	return &DistServer{
		hub: hub,
	}
}

// Emit sends a message to a specific client through gRPC.
// It looks up the client by ID and sends the message if the client exists.
// Returns success status indicating if the message was sent.
func (c *DistServer) Emit(_ context.Context, request *websocketpb.EmitRequest) (response *websocketpb.EmitResponse, err error) {
	response = &websocketpb.EmitResponse{}
	client, ok := c.hub.Client(request.GetId())
	if !ok {
		response.Success = false
		return
	}
	response.Success = client.Emit(request.GetData())
	return
}

// Online checks if a client is online through gRPC.
// It looks up the client by ID and returns whether it exists.
func (c *DistServer) Online(_ context.Context, request *websocketpb.OnlineRequest) (response *websocketpb.OnlineResponse, err error) {
	response = &websocketpb.OnlineResponse{}
	_, ok := c.hub.Client(request.Id)
	response.Online = ok
	return
}

// Broadcast sends a message to all clients through gRPC.
// It broadcasts the message to all locally connected clients.
func (c *DistServer) Broadcast(_ context.Context, request *websocketpb.BroadcastRequest) (response *websocketpb.BroadcastResponse, err error) {
	response = &websocketpb.BroadcastResponse{}
	c.hub.Broadcast(request.Data)
	response.Count = 1
	return
}

// DistSession represents a distributed WebSocket session.
// It wraps a Client and integrates with the Storage to track client locations.
type DistSession struct {
	// client is the underlying WebSocket client
	client *Client

	// storage is used to store and retrieve client location information
	storage Storage

	// addr is the address of this server node
	addr string
}

// NewDistSession creates a new DistSession instance with the given hub, storage, and address.
// Additional client options can be provided.
func NewDistSession(hub *Hub, storage Storage, addr string, opts ...Option) *DistSession {
	return &DistSession{
		client:  NewClient(hub, opts...),
		storage: storage,
		addr:    addr,
	}
}

// OnEvent sets the callback for handling incoming WebSocket messages.
// This wraps the client's OnEvent method.
func (c *DistSession) OnEvent(handler func(conn *Client, messageType int, message []byte)) {
	c.client.OnEvent(func(conn *Client, messageType int, message []byte) {
		handler(conn, messageType, message)
	})
}

// OnConnect sets the callback for when a WebSocket connection is established.
// It stores the client location in the storage and then calls the provided handler.
func (c *DistSession) OnConnect(handler func(conn *Client)) {
	c.client.OnConnect(func(conn *Client) {
		if err := c.storage.Set(conn.GetID(), c.addr); err != nil {
			c.client.error(err)
		}
		handler(conn)
	})
}

// OnError sets the callback for handling WebSocket errors.
// This wraps the client's OnError method.
func (c *DistSession) OnError(handler func(id string, err error)) {
	c.client.OnError(handler)
}

// OnDisconnect sets the callback for when a WebSocket connection is closed.
// It removes the client from the storage and then calls the provided handler.
func (c *DistSession) OnDisconnect(handler func(id string)) {
	c.client.OnDisconnect(func(id string) {
		if err := c.storage.Del(id); err != nil {
			c.client.error(err)
		}
		if handler != nil {
			handler(id)
		}
	})
}

// Conn upgrades the HTTP connection to a WebSocket connection and starts the session.
// This wraps the client's Conn method.
func (c *DistSession) Conn(w http.ResponseWriter, r *http.Request) error {
	return c.client.Conn(w, r)
}

// DistClient represents a client for distributed WebSocket operations.
// It is used by one server node to communicate with clients connected to other nodes.
type DistClient struct {
	// storage is used to look up where clients are connected
	storage Storage
}

// NewDistClient creates a new DistClient instance with the given storage.
func NewDistClient(storage Storage) *DistClient {
	return &DistClient{
		storage: storage,
	}
}

// grpcClientPool stores gRPC client connections to other server nodes.
// It uses sync.Map for thread-safe concurrent access.
var grpcClientPool sync.Map

// grpcClientConn gets or creates a gRPC client connection to the specified address.
// It manages connection reuse and cleanup, replacing stale connections.
func grpcClientConn(addr string) (*grpc.ClientConn, error) {
	instance, ok := grpcClientPool.Load(addr)
	if ok {
		switch instance.(*grpc.ClientConn).GetState() {
		case connectivity.Idle, connectivity.Ready, connectivity.Connecting:
			return instance.(*grpc.ClientConn), nil
		default:
			grpcClientPool.Delete(addr)
			_ = instance.(*grpc.ClientConn).Close()
		}
	}
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	grpcClientPool.Store(addr, conn)
	return conn, nil
}

// Emit sends a message to a specific client in the distributed system.
// It looks up the client's location, connects to that node, and sends the message.
// Returns whether the message was successfully sent.
func (c *DistClient) Emit(ctx context.Context, id string, message []byte) (ok bool, err error) {
	addr, err := c.storage.Get(id)
	if err != nil {
		return
	}
	if addr == "" {
		err = fmt.Errorf("%s not found", id)
		return
	}
	conn, err := grpcClientConn(addr)
	if err != nil {
		return
	}
	response, err := websocketpb.NewWebsocketClient(conn).Emit(ctx, &websocketpb.EmitRequest{
		Id:   id,
		Data: message,
	})
	if err != nil {
		return
	}
	return response.Success, nil
}

// Online checks if a client is online in the distributed system.
// It looks up the client's location and asks that node if the client is connected.
// Returns whether the client is online.
func (c *DistClient) Online(ctx context.Context, id string) (ok bool, err error) {
	addr, err := c.storage.Get(id)
	if err != nil {
		return
	}
	if addr == "" {
		err = fmt.Errorf("%s not found", id)
		return
	}
	conn, err := grpcClientConn(addr)
	if err != nil {
		return
	}
	response, err := websocketpb.NewWebsocketClient(conn).Online(ctx, &websocketpb.OnlineRequest{Id: id})
	if err != nil {
		return
	}
	return response.Online, nil
}

// Broadcast sends a message to all clients in the distributed system.
// It retrieves all client locations and sends the message to each node.
// Returns the number of successful sends.
func (c *DistClient) Broadcast(ctx context.Context, message []byte) (count int64, err error) {
	addrs, err := c.storage.All()
	if err != nil {
		return
	}
	remove := make([]string, 0)
	for id, addr := range addrs {
		conn, err := grpcClientConn(addr)
		if err != nil {
			continue
		}
		response, err := websocketpb.NewWebsocketClient(conn).Emit(ctx, &websocketpb.EmitRequest{
			Id:   id,
			Data: message,
		})
		if err != nil {
			continue
		}
		if response.Success {
			count++
		} else {
			remove = append(remove, id)
		}
	}
	if len(remove) > 0 {
		err = c.storage.Del(remove...)
	}
	return
}
