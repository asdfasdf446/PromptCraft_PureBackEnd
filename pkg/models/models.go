package models

const MapSize = 30

type Point struct {
	X int `json:"x"`
	Y int `json:"y"`
}

type Player struct {
	ID     string `json:"id"`
	Pos    Point  `json:"pos"`
	Symbol string `json:"symbol"`
	Health int    `json:"health"`
}

type WorldState struct {
	Map     [][]string `json:"map"`
	Players []Player   `json:"players"`
	Logs    []string   `json:"logs"`
}

type Command struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
}
