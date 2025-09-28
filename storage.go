package websocket

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
