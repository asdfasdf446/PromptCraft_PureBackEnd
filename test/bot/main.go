package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

// Mirrors the API types (lightweight copy to avoid importing internal packages).
type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type MapCell struct {
	X        int      `json:"x"`
	Y        int      `json:"y"`
	Walkable bool     `json:"walkable"`
	HasPlant bool     `json:"has_plant"`
	Players  []string `json:"players"`
}

type MapResponse struct {
	Cells []MapCell `json:"cells"`
}

type PlayerStatus struct {
	ID         string `json:"id"`
	Pos        Point  `json:"pos"`
	Health     int    `json:"health"`
	MaxHP      int    `json:"max_hp"`
	AP         int    `json:"ap"`
	MaxAP      int    `json:"max_ap"`
	Satiety    int    `json:"satiety"`
	MaxSatiety int    `json:"max_satiety"`
	Alive      bool   `json:"alive"`
}

type StatusResponse struct {
	Player  PlayerStatus   `json:"player"`
	Players []PlayerStatus `json:"players"`
}

type CommandResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type JoinResponse struct {
	PlayerID string `json:"player_id"`
}

const (
	weakHP      = 5
	weakSatiety = 4
	strongHP   = 7
	strongSat  = 7
)

func main() {
	port := "8080"
	if len(os.Args) >= 2 {
		port = os.Args[1]
	}
	baseURL := "http://localhost:" + port

	// 1. Join the game
	joinResp, err := apiPost[JoinResponse](baseURL+"/api/join", map[string]string{"symbol": "机器人"})
	if err != nil {
		log.Fatalf("Failed to join: %v", err)
	}
	playerID := joinResp.PlayerID
	log.Printf("🤖 Bot %s joined the game", playerID)

	// 2. Main loop
	for {
		status, err := apiGet[StatusResponse](fmt.Sprintf("%s/api/status?player_id=%s", baseURL, playerID))
		if err != nil {
			log.Printf("Status error: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		if !status.Player.Alive {
			log.Println("☠ Bot died. Exiting.")
			return
		}

		gameMap, err := apiGet[MapResponse](baseURL + "/api/map")
		if err != nil {
			log.Printf("Map error: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		action := decide(status.Player, status.Players, gameMap.Cells)
		if action != "" {
			resp, err := apiPost[CommandResponse](baseURL+"/api/command", map[string]string{
				"player_id": playerID,
				"command":   action,
			})
			if err != nil {
				log.Printf("Command error: %v", err)
			} else {
				log.Printf("[%s] %s → %s", status.Player.ID, action, resp.Message)
			}
		}

		time.Sleep(1200 * time.Millisecond)
	}
}

// ============================================================================
// Decision engine
// ============================================================================

func decide(me PlayerStatus, allPlayers []PlayerStatus, cells []MapCell) string {
	isWeak := me.Health < weakHP || me.Satiety < weakSatiety
	isStrong := me.Health >= strongHP && me.Satiety >= strongSat

	if isWeak {
		return decideWeak(me, allPlayers, cells)
	}
	if isStrong {
		return decideStrong(me, allPlayers, cells)
	}
	return decideNormal(me, cells)
}

func decideWeak(me PlayerStatus, allPlayers []PlayerStatus, cells []MapCell) string {
	// If a plant is on current tile, eat it
	if cell := cellAt(cells, me.Pos.X, me.Pos.Y); cell != nil && cell.HasPlant {
		return "eat"
	}

	// If another player is close (within 2 tiles), run away
	if threat := nearestPlayer(me.Pos, me.ID, cells, 2); threat != nil {
		return moveAway(me.Pos, threat.X, threat.Y)
	}

	// Move toward nearest plant
	if target := nearestPlant(me.Pos, cells); target != nil {
		// If adjacent to plant, eat it
		if dist(me.Pos, Point{target.X, target.Y}) == 1 {
			return "eat " + directionTo(me.Pos, Point{target.X, target.Y})
		}
		return "move " + directionTo(me.Pos, Point{target.X, target.Y})
	}

	// No plants found, explore
	return wander(me.Pos)
}

func decideStrong(me PlayerStatus, allPlayers []PlayerStatus, cells []MapCell) string {
	// Find nearest other player
	if target := nearestPlayer(me.Pos, me.ID, cells, 10); target != nil {
		d := dist(me.Pos, Point{target.X, target.Y})
		if d <= 1 {
			if d == 0 {
				return "attack"
			}
			return "attack " + directionTo(me.Pos, Point{target.X, target.Y})
		}
		return "move " + directionTo(me.Pos, Point{target.X, target.Y})
	}

	// No players nearby, eat to maintain strength
	return decideNormal(me, cells)
}

func decideNormal(me PlayerStatus, cells []MapCell) string {
	if cell := cellAt(cells, me.Pos.X, me.Pos.Y); cell != nil && cell.HasPlant {
		return "eat"
	}
	if target := nearestPlant(me.Pos, cells); target != nil {
		if dist(me.Pos, Point{target.X, target.Y}) == 1 {
			return "eat " + directionTo(me.Pos, Point{target.X, target.Y})
		}
		return "move " + directionTo(me.Pos, Point{target.X, target.Y})
	}
	return wander(me.Pos)
}

// ============================================================================
// Helpers
// ============================================================================

func cellAt(cells []MapCell, x, y int) *MapCell {
	for i := range cells {
		if cells[i].X == x && cells[i].Y == y {
			return &cells[i]
		}
	}
	return nil
}

func nearestPlant(from Point, cells []MapCell) *MapCell {
	var best *MapCell
	bestD := 999
	for i := range cells {
		c := &cells[i]
		if !c.HasPlant || !c.Walkable {
			continue
		}
		d := dist(from, Point{c.X, c.Y})
		if d < bestD {
			bestD = d
			best = c
		}
	}
	return best
}

func nearestPlayer(from Point, myID string, cells []MapCell, maxDist int) *MapCell {
	var best *MapCell
	bestD := maxDist + 1
	for i := range cells {
		c := &cells[i]
		if len(c.Players) == 0 {
			continue
		}
		// Check if any player here is not me
		hasOther := false
		for _, pid := range c.Players {
			if pid != myID {
				hasOther = true
				break
			}
		}
		if !hasOther {
			continue
		}
		d := dist(from, Point{c.X, c.Y})
		if d <= maxDist && d < bestD && d > 0 {
			bestD = d
			best = c
		}
	}
	return best
}

func moveAway(from Point, threatX, threatY int) string {
	// Move in the opposite direction from the threat
	dx := from.X - threatX
	dy := from.Y - threatY
	if abs(dx) > abs(dy) {
		if dx > 0 {
			return "move east"
		}
		return "move west"
	}
	if dy > 0 {
		return "move south"
	}
	return "move north"
}

func directionTo(from, to Point) string {
	dx := to.X - from.X
	dy := to.Y - from.Y
	if abs(dx) > abs(dy) {
		if dx > 0 {
			return "east"
		}
		return "west"
	}
	if dy > 0 {
		return "south"
	}
	return "north"
}

var wanderDir = []string{"north", "south", "east", "west"}
var wanderIdx int

func wander(pos Point) string {
	wanderIdx = (wanderIdx + 1) % len(wanderDir)
	return "move " + wanderDir[wanderIdx]
}

func dist(a, b Point) int {
	return abs(a.X-b.X) + abs(a.Y-b.Y)
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// ============================================================================
// HTTP helpers
// ============================================================================

func apiGet[T any](url string) (T, error) {
	var zero T
	resp, err := http.Get(url)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result T
	if err := json.Unmarshal(body, &result); err != nil {
		return zero, fmt.Errorf("unmarshal: %w (body: %s)", err, string(body))
	}
	return result, nil
}

func apiPost[T any](url string, payload interface{}) (T, error) {
	var zero T
	data, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var result T
	if err := json.Unmarshal(body, &result); err != nil {
		return zero, fmt.Errorf("unmarshal: %w (body: %s)", err, string(body))
	}
	return result, nil
}
