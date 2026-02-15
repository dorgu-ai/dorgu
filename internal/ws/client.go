package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// MessageType defines the type of WebSocket message.
type MessageType string

const (
	MessageTypeSubscribe   MessageType = "subscribe"
	MessageTypeUnsubscribe MessageType = "unsubscribe"
	MessageTypeRequest     MessageType = "request"
	MessageTypeEvent       MessageType = "event"
	MessageTypeResponse    MessageType = "response"
	MessageTypeError       MessageType = "error"
)

// Topic defines the subscription topic.
type Topic string

const (
	TopicPersonas    Topic = "personas"
	TopicCluster     Topic = "cluster"
	TopicDeployments Topic = "deployments"
	TopicEvents      Topic = "events"
)

// Message is the base WebSocket message structure.
type Message struct {
	Type      MessageType     `json:"type"`
	Topic     Topic           `json:"topic,omitempty"`
	RequestID string          `json:"requestId,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// PersonaEvent represents a persona change event.
type PersonaEvent struct {
	EventType string `json:"eventType"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Phase     string `json:"phase,omitempty"`
	Health    string `json:"health,omitempty"`
}

// ClusterEvent represents a cluster state change event.
type ClusterEvent struct {
	EventType        string `json:"eventType"`
	Name             string `json:"name"`
	Phase            string `json:"phase,omitempty"`
	NodeCount        int    `json:"nodeCount,omitempty"`
	ApplicationCount int    `json:"applicationCount,omitempty"`
}

// PersonaSummary is a summary of an ApplicationPersona.
type PersonaSummary struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	AppName   string `json:"appName"`
	Type      string `json:"type"`
	Tier      string `json:"tier"`
	Phase     string `json:"phase"`
	Health    string `json:"health"`
}

// ListPersonasResponse is the response for listing personas.
type ListPersonasResponse struct {
	Personas []PersonaSummary `json:"personas"`
}

// ClusterResponse is the response for cluster info.
type ClusterResponse struct {
	Name             string   `json:"name"`
	Environment      string   `json:"environment"`
	Phase            string   `json:"phase"`
	KubernetesVer    string   `json:"kubernetesVersion"`
	Platform         string   `json:"platform"`
	NodeCount        int      `json:"nodeCount"`
	ApplicationCount int      `json:"applicationCount"`
	Addons           []string `json:"addons"`
}

// ErrorPayload is the payload for error messages.
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Client is a WebSocket client for communicating with the Dorgu Operator.
type Client struct {
	url           string
	conn          *websocket.Conn
	connected     bool
	mu            sync.RWMutex
	handlers      map[Topic]func(*Message)
	handlersMu    sync.RWMutex
	responses     map[string]chan *Message
	responsesMu   sync.Mutex
	done          chan struct{}
	reconnectWait time.Duration
}

// NewClient creates a new WebSocket client.
func NewClient(url string) *Client {
	return &Client{
		url:           url,
		handlers:      make(map[Topic]func(*Message)),
		responses:     make(map[string]chan *Message),
		done:          make(chan struct{}),
		reconnectWait: 5 * time.Second,
	}
}

// Connect establishes a WebSocket connection.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, c.url, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", c.url, err)
	}

	c.conn = conn
	c.connected = true

	// Start read pump
	go c.readPump()

	return nil
}

// Close closes the WebSocket connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil
	}

	close(c.done)
	c.connected = false

	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// IsConnected returns whether the client is connected.
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// Subscribe subscribes to a topic.
func (c *Client) Subscribe(ctx context.Context, topic Topic, handler func(*Message)) error {
	c.handlersMu.Lock()
	c.handlers[topic] = handler
	c.handlersMu.Unlock()

	msg := &Message{
		Type:      MessageTypeSubscribe,
		Topic:     topic,
		RequestID: generateRequestID(),
		Timestamp: time.Now(),
	}

	return c.send(msg)
}

// Unsubscribe unsubscribes from a topic.
func (c *Client) Unsubscribe(ctx context.Context, topic Topic) error {
	c.handlersMu.Lock()
	delete(c.handlers, topic)
	c.handlersMu.Unlock()

	msg := &Message{
		Type:      MessageTypeUnsubscribe,
		Topic:     topic,
		RequestID: generateRequestID(),
		Timestamp: time.Now(),
	}

	return c.send(msg)
}

// ListPersonas requests a list of personas.
func (c *Client) ListPersonas(ctx context.Context, namespace string) (*ListPersonasResponse, error) {
	payload := map[string]string{}
	if namespace != "" {
		payload["namespace"] = namespace
	}

	payloadBytes, _ := json.Marshal(payload)
	msg := &Message{
		Type:      MessageTypeRequest,
		Topic:     TopicPersonas,
		RequestID: generateRequestID(),
		Payload:   payloadBytes,
		Timestamp: time.Now(),
	}

	resp, err := c.request(ctx, msg)
	if err != nil {
		return nil, err
	}

	var result ListPersonasResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// GetCluster requests cluster information.
func (c *Client) GetCluster(ctx context.Context, name string) (*ClusterResponse, error) {
	payload := map[string]string{}
	if name != "" {
		payload["name"] = name
	}

	payloadBytes, _ := json.Marshal(payload)
	msg := &Message{
		Type:      MessageTypeRequest,
		Topic:     TopicCluster,
		RequestID: generateRequestID(),
		Payload:   payloadBytes,
		Timestamp: time.Now(),
	}

	resp, err := c.request(ctx, msg)
	if err != nil {
		return nil, err
	}

	var result ClusterResponse
	if err := json.Unmarshal(resp.Payload, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// request sends a request and waits for a response.
func (c *Client) request(ctx context.Context, msg *Message) (*Message, error) {
	respChan := make(chan *Message, 1)

	c.responsesMu.Lock()
	c.responses[msg.RequestID] = respChan
	c.responsesMu.Unlock()

	defer func() {
		c.responsesMu.Lock()
		delete(c.responses, msg.RequestID)
		c.responsesMu.Unlock()
	}()

	if err := c.send(msg); err != nil {
		return nil, err
	}

	select {
	case resp := <-respChan:
		if resp.Type == MessageTypeError {
			var errPayload ErrorPayload
			json.Unmarshal(resp.Payload, &errPayload)
			return nil, fmt.Errorf("%s: %s", errPayload.Code, errPayload.Message)
		}
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
		return nil, fmt.Errorf("request timeout")
	}
}

// send sends a message over the WebSocket connection.
func (c *Client) send(msg *Message) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected || c.conn == nil {
		return fmt.Errorf("not connected")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return c.conn.WriteMessage(websocket.TextMessage, data)
}

// readPump reads messages from the WebSocket connection.
func (c *Client) readPump() {
	defer func() {
		c.mu.Lock()
		c.connected = false
		if c.conn != nil {
			c.conn.Close()
		}
		c.mu.Unlock()
	}()

	for {
		select {
		case <-c.done:
			return
		default:
		}

		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				// Log error if needed
			}
			return
		}

		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		c.handleMessage(&msg)
	}
}

// handleMessage handles incoming messages.
func (c *Client) handleMessage(msg *Message) {
	// Check if this is a response to a pending request
	if msg.RequestID != "" {
		c.responsesMu.Lock()
		if respChan, ok := c.responses[msg.RequestID]; ok {
			select {
			case respChan <- msg:
			default:
			}
		}
		c.responsesMu.Unlock()
	}

	// Call topic handler for events
	if msg.Type == MessageTypeEvent {
		c.handlersMu.RLock()
		if handler, ok := c.handlers[msg.Topic]; ok {
			go handler(msg)
		}
		c.handlersMu.RUnlock()
	}
}

// generateRequestID generates a unique request ID.
func generateRequestID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
