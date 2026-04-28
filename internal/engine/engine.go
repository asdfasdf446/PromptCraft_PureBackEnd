package engine

import (
	"fmt"
	"promptcraft/pkg/models"
	"strings"
	"sync"
)

type GameEngine struct {
	mu    sync.RWMutex
	state models.WorldState
}

func NewGameEngine() *GameEngine {
	grid := make([][]string, models.MapSize)
	for i := range grid {
		grid[i] = make([]string, models.MapSize)
		for j := range grid[i] {
			grid[i][j] = "."
		}
	}

	return &GameEngine{
		state: models.WorldState{
			Map: grid,
			Players: []models.Player{
				{ID: "p1", Pos: models.Point{X: 5, Y: 5}, Symbol: "@", Health: 100},
			},
			Logs: []string{"Welcome to PromptCraft!"},
		},
	}
}

func (e *GameEngine) GetState() models.WorldState {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.state
}

func (e *GameEngine) ProcessCommand(rawCmd string) (string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	parts := strings.Fields(strings.ToLower(rawCmd))
	if len(parts) == 0 {
		return "Empty command", false
	}

	action := parts[0]
	player := &e.state.Players[0]

	var msg string
	var success bool

	switch action {
	case "move":
		if len(parts) < 2 {
			msg = "Move where? (north, south, east, west)"
			success = false
		} else {
			msg, success = e.handleMove(player, parts[1])
		}
	case "attack":
		msg = "You swing at the air!"
		success = true
	default:
		msg = fmt.Sprintf("Unknown command: %s", action)
		success = false
	}

	e.state.Logs = append(e.state.Logs, msg)
	if len(e.state.Logs) > 100 {
		e.state.Logs = e.state.Logs[len(e.state.Logs)-100:]
	}
	return msg, success
}

func (e *GameEngine) handleMove(p *models.Player, dir string) (string, bool) {
	newPos := p.Pos
	switch dir {
	case "north":
		newPos.Y--
	case "south":
		newPos.Y++
	case "east":
		newPos.X++
	case "west":
		newPos.X--
	default:
		return "Invalid direction. Use north, south, east, west.", false
	}

	if newPos.X < 0 || newPos.X >= models.MapSize || newPos.Y < 0 || newPos.Y >= models.MapSize {
		return "You hit the world border!", false
	}

	p.Pos = newPos
	return fmt.Sprintf("Moved %s to (%d, %d)", dir, p.Pos.X, p.Pos.Y), true
}
