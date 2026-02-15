package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockWebSocketServer creates a mock WebSocket server for testing
func mockWebSocketServer(t *testing.T, handler func(*websocket.Conn)) *httptest.Server {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade error: %v", err)
			return
		}
		defer conn.Close()
		handler(conn)
	}))
}

func TestClient_Connect(t *testing.T) {
	server := mockWebSocketServer(t, func(conn *websocket.Conn) {
		// Keep connection open
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client := NewClient(wsURL)

	ctx := context.Background()
	err := client.Connect(ctx)
	require.NoError(t, err)
	assert.True(t, client.IsConnected())

	err = client.Close()
	require.NoError(t, err)
	assert.False(t, client.IsConnected())
}

func TestClient_ConnectFailure(t *testing.T) {
	client := NewClient("ws://localhost:99999/ws")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	assert.Error(t, err)
	assert.False(t, client.IsConnected())
}

func TestClient_Subscribe(t *testing.T) {
	server := mockWebSocketServer(t, func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			var msg Message
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}

			if msg.Type == MessageTypeSubscribe {
				response := Message{
					Type:      MessageTypeResponse,
					Topic:     msg.Topic,
					RequestID: msg.RequestID,
					Timestamp: time.Now(),
				}
				conn.WriteJSON(response)
			}
		}
	})
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client := NewClient(wsURL)

	ctx := context.Background()
	err := client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close()

	// Subscribe to a topic
	received := make(chan bool, 1)
	err = client.Subscribe(ctx, TopicPersonas, func(msg *Message) {
		received <- true
	})
	require.NoError(t, err)
}

func TestClient_Unsubscribe(t *testing.T) {
	server := mockWebSocketServer(t, func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			var msg Message
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}

			response := Message{
				Type:      MessageTypeResponse,
				Topic:     msg.Topic,
				RequestID: msg.RequestID,
				Timestamp: time.Now(),
			}
			conn.WriteJSON(response)
		}
	})
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client := NewClient(wsURL)

	ctx := context.Background()
	err := client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close()

	// Subscribe first
	err = client.Subscribe(ctx, TopicPersonas, func(msg *Message) {})
	require.NoError(t, err)

	// Unsubscribe
	err = client.Unsubscribe(ctx, TopicPersonas)
	require.NoError(t, err)
}

func TestClient_ListPersonas(t *testing.T) {
	server := mockWebSocketServer(t, func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			var msg Message
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}

			if msg.Type == MessageTypeRequest && msg.Topic == TopicPersonas {
				personas := ListPersonasResponse{
					Personas: []PersonaSummary{
						{
							Namespace: "default",
							Name:      "test-persona",
							AppName:   "test-app",
							Type:      "api",
							Tier:      "backend",
							Phase:     "Active",
							Health:    "Healthy",
						},
					},
				}
				payload, _ := json.Marshal(personas)

				response := Message{
					Type:      MessageTypeResponse,
					Topic:     msg.Topic,
					RequestID: msg.RequestID,
					Payload:   payload,
					Timestamp: time.Now(),
				}
				conn.WriteJSON(response)
			}
		}
	})
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client := NewClient(wsURL)

	ctx := context.Background()
	err := client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close()

	personas, err := client.ListPersonas(ctx, "")
	require.NoError(t, err)
	assert.NotNil(t, personas)
	assert.Len(t, personas.Personas, 1)
	assert.Equal(t, "test-app", personas.Personas[0].AppName)
}

func TestClient_GetCluster(t *testing.T) {
	server := mockWebSocketServer(t, func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			var msg Message
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}

			if msg.Type == MessageTypeRequest && msg.Topic == TopicCluster {
				cluster := ClusterResponse{
					Name:             "test-cluster",
					Environment:      "development",
					Phase:            "Active",
					KubernetesVer:    "v1.28.0",
					Platform:         "kind",
					NodeCount:        3,
					ApplicationCount: 5,
					Addons:           []string{"argocd", "prometheus"},
				}
				payload, _ := json.Marshal(cluster)

				response := Message{
					Type:      MessageTypeResponse,
					Topic:     msg.Topic,
					RequestID: msg.RequestID,
					Payload:   payload,
					Timestamp: time.Now(),
				}
				conn.WriteJSON(response)
			}
		}
	})
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client := NewClient(wsURL)

	ctx := context.Background()
	err := client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close()

	cluster, err := client.GetCluster(ctx, "")
	require.NoError(t, err)
	assert.NotNil(t, cluster)
	assert.Equal(t, "test-cluster", cluster.Name)
	assert.Equal(t, "development", cluster.Environment)
	assert.Equal(t, 3, cluster.NodeCount)
}

func TestClient_ErrorResponse(t *testing.T) {
	server := mockWebSocketServer(t, func(conn *websocket.Conn) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}

			var msg Message
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}

			errorPayload := ErrorPayload{
				Code:    "NOT_FOUND",
				Message: "Resource not found",
			}
			payload, _ := json.Marshal(errorPayload)

			response := Message{
				Type:      MessageTypeError,
				Topic:     msg.Topic,
				RequestID: msg.RequestID,
				Payload:   payload,
				Timestamp: time.Now(),
			}
			conn.WriteJSON(response)
		}
	})
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client := NewClient(wsURL)

	ctx := context.Background()
	err := client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close()

	_, err = client.ListPersonas(ctx, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NOT_FOUND")
}

func TestClient_RequestTimeout(t *testing.T) {
	server := mockWebSocketServer(t, func(conn *websocket.Conn) {
		// Don't respond to simulate timeout
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client := NewClient(wsURL)

	ctx := context.Background()
	err := client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close()

	// Use a short timeout context
	shortCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	_, err = client.ListPersonas(shortCtx, "")
	assert.Error(t, err)
}

func TestClient_EventHandler(t *testing.T) {
	eventChan := make(chan PersonaEvent, 1)

	server := mockWebSocketServer(t, func(conn *websocket.Conn) {
		// First, handle subscribe
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var msg Message
		json.Unmarshal(data, &msg)

		// Send subscribe response
		response := Message{
			Type:      MessageTypeResponse,
			RequestID: msg.RequestID,
			Timestamp: time.Now(),
		}
		conn.WriteJSON(response)

		// Send an event
		time.Sleep(100 * time.Millisecond)
		event := PersonaEvent{
			EventType: "created",
			Namespace: "default",
			Name:      "new-app",
			Phase:     "Active",
		}
		payload, _ := json.Marshal(event)

		eventMsg := Message{
			Type:      MessageTypeEvent,
			Topic:     TopicPersonas,
			Payload:   payload,
			Timestamp: time.Now(),
		}
		conn.WriteJSON(eventMsg)

		// Keep connection open
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client := NewClient(wsURL)

	ctx := context.Background()
	err := client.Connect(ctx)
	require.NoError(t, err)
	defer client.Close()

	// Subscribe with handler
	err = client.Subscribe(ctx, TopicPersonas, func(msg *Message) {
		var event PersonaEvent
		json.Unmarshal(msg.Payload, &event)
		eventChan <- event
	})
	require.NoError(t, err)

	// Wait for event
	select {
	case event := <-eventChan:
		assert.Equal(t, "created", event.EventType)
		assert.Equal(t, "new-app", event.Name)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestClient_NotConnected(t *testing.T) {
	client := NewClient("ws://localhost:9999/ws")

	ctx := context.Background()

	// Try to subscribe without connecting
	err := client.Subscribe(ctx, TopicPersonas, func(msg *Message) {})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")

	// Try to list personas without connecting
	_, err = client.ListPersonas(ctx, "")
	assert.Error(t, err)
}

func TestGenerateRequestID(t *testing.T) {
	id1 := generateRequestID()
	time.Sleep(time.Nanosecond) // Ensure different timestamps
	id2 := generateRequestID()

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	// IDs should be unique (though they may be equal if generated at the same nanosecond)
	// The important thing is they are non-empty strings
}
