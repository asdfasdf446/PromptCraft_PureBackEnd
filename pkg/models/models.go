package models

import (
	"encoding/json"
	"fmt"
)

const MapSize = 10

// ============================================================================
// Terrain System
// ============================================================================

type TerrainType int

const (
	TerrainGround   TerrainType = iota // walkable
	TerrainObstacle                    // blocks movement
	TerrainSoil                        // walkable, can grow plants
)

type terrainInfo struct {
	Name     string
	Walkable bool
}

var terrainRegistry = map[TerrainType]terrainInfo{
	TerrainGround:   {Name: "地面", Walkable: true},
	TerrainObstacle: {Name: "障碍", Walkable: false},
	TerrainSoil:     {Name: "土壤", Walkable: true},
}

type Terrain struct {
	Type TerrainType `json:"type"`
}

func NewTerrain(t TerrainType) Terrain {
	return Terrain{Type: t}
}

func (t Terrain) Name() string {
	if info, ok := terrainRegistry[t.Type]; ok {
		return info.Name
	}
	return "未知"
}

func (t Terrain) IsWalkable() bool {
	if info, ok := terrainRegistry[t.Type]; ok {
		return info.Walkable
	}
	return false
}

func (t Terrain) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Type     int    `json:"type"`
		Name     string `json:"name"`
		Walkable bool   `json:"walkable"`
	}{
		Type:     int(t.Type),
		Name:     t.Name(),
		Walkable: t.IsWalkable(),
	})
}

func (t *Terrain) UnmarshalJSON(data []byte) error {
	var aux struct {
		Type int `json:"type"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	t.Type = TerrainType(aux.Type)
	return nil
}

// ============================================================================
// Unit System
// ============================================================================

// Unit is the base type for all world entities (players, plants, etc.).
// Sub-types embed Unit and add type-specific fields.
type Unit struct {
	ID     string `json:"id"`
	Pos    Point  `json:"pos"`
	Symbol string `json:"symbol"`
	Health int    `json:"health"`
	MaxHP  int    `json:"max_hp"`
}

func (u Unit) IsAlive() bool { return u.Health > 0 }

// ============================================================================
// Player (extends Unit)
// ============================================================================

type Player struct {
	Unit
	AP         int  `json:"ap"`
	MaxAP      int  `json:"max_ap"`
	Satiety    int  `json:"satiety"`
	MaxSatiety int  `json:"max_satiety"`
}

func NewPlayer(id string, pos Point) Player {
	return Player{
		Unit: Unit{
			ID: id, Pos: pos, Symbol: "玩家",
			Health: 10, MaxHP: 10,
		},
		AP: 10, MaxAP: 10,
		Satiety: 10, MaxSatiety: 10,
	}
}

func (p Player) IsDead() bool { return p.Health <= 0 }

// ============================================================================
// Plant (extends Unit)
// ============================================================================

type Plant struct {
	Unit
}

func NewPlant(id string, pos Point) Plant {
	return Plant{
		Unit: Unit{
			ID: id, Pos: pos, Symbol: "植物",
			Health: 1, MaxHP: 1,
		},
	}
}

// ============================================================================
// Geometry & Cell
// ============================================================================

type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type Cell struct {
	Terrain  Terrain  `json:"terrain"`
	Entities []string `json:"entities"`
}

// ============================================================================
// World State
// ============================================================================

type WorldState struct {
	Map     [][]Cell `json:"map"`
	Players []Player `json:"players"`
	Plants  []Plant  `json:"plants"`
	Logs    []string `json:"logs"`
	Time    *GameTime `json:"time"`
}

// ============================================================================
// Command
// ============================================================================

type Command struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

// ============================================================================
// Game Time
// ============================================================================

type GameTime struct {
	seconds int
}

func NewGameTime() *GameTime {
	return &GameTime{seconds: 0}
}

func (gt *GameTime) Advance() {
	gt.seconds++
}

func (gt *GameTime) TotalSeconds() int {
	return gt.seconds
}

func (gt *GameTime) Day() int {
	return gt.seconds/86400 + 1
}

func (gt *GameTime) Hour() int {
	return (gt.seconds % 86400) / 3600
}

func (gt *GameTime) Minute() int {
	return (gt.seconds % 3600) / 60
}

func (gt *GameTime) Second() int {
	return gt.seconds % 60
}

func (gt *GameTime) Format() string {
	return fmt.Sprintf("Day %d, %02d:%02d:%02d",
		gt.Day(), gt.Hour(), gt.Minute(), gt.Second())
}

func (gt *GameTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Seconds int    `json:"seconds"`
		Display string `json:"display"`
	}{
		Seconds: gt.seconds,
		Display: gt.Format(),
	})
}

func (gt *GameTime) UnmarshalJSON(data []byte) error {
	var aux struct {
		Seconds int `json:"seconds"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	gt.seconds = aux.Seconds
	return nil
}

// ============================================================================
// API Types
// ============================================================================

// CommandRequest is the JSON body for POST /api/command.
type CommandRequest struct {
	PlayerID string `json:"player_id"`
	Command  string `json:"command"`
}

// CommandResponse is returned after executing a command.
type CommandResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// MapCell is a flattened cell view for the map API.
type MapCell struct {
	X        int      `json:"x"`
	Y        int      `json:"y"`
	Terrain  string   `json:"terrain"`
	Type     int      `json:"type"`
	Walkable bool     `json:"walkable"`
	HasPlant bool     `json:"has_plant"`
	Players  []string `json:"players"`
}

// MapResponse is returned by GET /api/map.
type MapResponse struct {
	Size  int       `json:"size"`
	Cells []MapCell `json:"cells"`
}

// PlayerStatus is returned by GET /api/status.
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

// StatusResponse is returned by GET /api/status.
type StatusResponse struct {
	Player  PlayerStatus `json:"player"`
	Players []PlayerStatus `json:"players,omitempty"`
	Time    string       `json:"time"`
}

// JoinRequest is the JSON body for POST /api/join.
type JoinRequest struct {
	Symbol string `json:"symbol"`
}

// JoinResponse is returned after joining.
type JoinResponse struct {
	PlayerID string `json:"player_id"`
	Pos      Point  `json:"pos"`
}
