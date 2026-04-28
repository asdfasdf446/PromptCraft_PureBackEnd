package network

import (
	"encoding/json"
	"log"
	"net/http"
	"promptcraft/internal/engine"
	"promptcraft/pkg/models"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type Server struct {
	engine *engine.GameEngine
	clients map[*websocket.Conn]bool
	mu      sync.Mutex
}

func NewServer(e *engine.GameEngine) *Server {
	return &Server{
		engine:  e,
		clients: make(map[*websocket.Conn]bool),
	}
}

func (s *Server) HandleConnections(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Upgrade error: %v", err)
		return
	}
	defer conn.Close()

	s.mu.Lock()
	s.clients[conn] = true
	s.mu.Unlock()

	// Send initial state
	s.BroadcastState()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			s.mu.Lock()
			delete(s.clients, conn)
			s.mu.Unlock()
			break
		}

		var cmd models.Command
		if err := json.Unmarshal(msg, &cmd); err != nil {
			continue
		}

		if cmd.Type == "command" {
			s.engine.ProcessCommand(cmd.Payload)
			s.BroadcastState()
		}
	}
}

func (s *Server) BroadcastState() {
	state := s.engine.GetState()
	payload, _ := json.Marshal(state)

	s.mu.Lock()
	defer s.mu.Unlock()

	for client := range s.clients {
		err := client.WriteMessage(websocket.TextMessage, payload)
		if err != nil {
			client.Close()
			delete(s.clients, client)
		}
	}
}
