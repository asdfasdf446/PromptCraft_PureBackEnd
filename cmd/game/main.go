package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"promptcraft/internal/engine"
	"promptcraft/internal/network"
	"promptcraft/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	port := flag.Int("port", 8080, "API/WebSocket server port")
	flag.Parse()

	gameEngine := engine.NewGameEngine()

	// Spawn a second player for API bots
	gameEngine.AddPlayer("机器人")

	ln, err := listenPort(*port)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	addr := ln.Addr().String()

	server := network.NewServer(gameEngine)
	http.HandleFunc("/ws", server.HandleConnections)

	// REST API endpoints
	http.HandleFunc("/api/command", server.HandleCommandAPI)
	http.HandleFunc("/api/map", server.HandleMapAPI)
	http.HandleFunc("/api/status", server.HandleStatusAPI)
	http.HandleFunc("/api/join", server.HandleJoinAPI)

	go gameEngine.StartTime(func() {
		server.BroadcastState()
	})
	defer gameEngine.StopTime()

	go func() {
		if err := http.Serve(ln, nil); err != nil {
		}
	}()

	// Print port prominently so it survives the TUI taking over stdout
	fmt.Fprintf(os.Stderr, "\n=== API server: %s ===\n\n", addr)

	p := tea.NewProgram(tui.NewModel(addr), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatalf("TUI Error: %v", err)
	}
}

func listenPort(preferred int) (net.Listener, error) {
	// Try the preferred port first, then try a few more
	for offset := 0; offset < 10; offset++ {
		addr := fmt.Sprintf("localhost:%d", preferred+offset)
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			return ln, nil
		}
	}
	return nil, fmt.Errorf("no available port in range %d-%d", preferred, preferred+9)
}
