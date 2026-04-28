package network

import (
	"encoding/json"
	"net/http"
	"promptcraft/pkg/models"
)

// HandleCommandAPI processes a command via REST API.
func (s *Server) HandleCommandAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req models.CommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, models.CommandResponse{
			Success: false, Message: "invalid request body",
		})
		return
	}

	msg, ok := s.engine.ProcessCommandForPlayer(req.PlayerID, req.Command)
	writeJSON(w, http.StatusOK, models.CommandResponse{
		Success: ok, Message: msg,
	})
}

// HandleMapAPI returns the full map state.
func (s *Server) HandleMapAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	state := s.engine.GetState()
	cells := make([]models.MapCell, 0, models.MapSize*models.MapSize)

	for y := 0; y < models.MapSize; y++ {
		for x := 0; x < models.MapSize; x++ {
			cell := state.Map[y][x]

			hasPlant := false
			for _, p := range state.Plants {
				if p.Pos.X == x && p.Pos.Y == y && p.Health > 0 {
					hasPlant = true
					break
				}
			}

			var playerIDs []string
			for _, p := range state.Players {
				if p.Pos.X == x && p.Pos.Y == y && !p.IsDead() {
					playerIDs = append(playerIDs, p.ID)
				}
			}

			cells = append(cells, models.MapCell{
				X:        x,
				Y:        y,
				Terrain:  cell.Terrain.Name(),
				Type:     int(cell.Terrain.Type),
				Walkable: cell.Terrain.IsWalkable(),
				HasPlant: hasPlant,
				Players:  playerIDs,
			})
		}
	}

	writeJSON(w, http.StatusOK, models.MapResponse{
		Size:  models.MapSize,
		Cells: cells,
	})
}

// HandleStatusAPI returns player status.
func (s *Server) HandleStatusAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	playerID := r.URL.Query().Get("player_id")
	state := s.engine.GetState()

	// Build all player statuses
	allStatuses := make([]models.PlayerStatus, 0, len(state.Players))
	for _, p := range state.Players {
		allStatuses = append(allStatuses, models.PlayerStatus{
			ID: p.ID, Pos: p.Pos,
			Health: p.Health, MaxHP: p.MaxHP,
			AP: p.AP, MaxAP: p.MaxAP,
			Satiety: p.Satiety, MaxSatiety: p.MaxSatiety,
			Alive: !p.IsDead(),
		})
	}

	var primary models.PlayerStatus
	if playerID != "" {
		for _, p := range state.Players {
			if p.ID == playerID {
				primary = models.PlayerStatus{
					ID: p.ID, Pos: p.Pos,
					Health: p.Health, MaxHP: p.MaxHP,
					AP: p.AP, MaxAP: p.MaxAP,
					Satiety: p.Satiety, MaxSatiety: p.MaxSatiety,
					Alive: !p.IsDead(),
				}
				break
			}
		}
	} else if len(state.Players) > 0 {
		p := state.Players[0]
		primary = models.PlayerStatus{
			ID: p.ID, Pos: p.Pos,
			Health: p.Health, MaxHP: p.MaxHP,
			AP: p.AP, MaxAP: p.MaxAP,
			Satiety: p.Satiety, MaxSatiety: p.MaxSatiety,
			Alive: !p.IsDead(),
		}
	}

	writeJSON(w, http.StatusOK, models.StatusResponse{
		Player:  primary,
		Players: allStatuses,
		Time:    state.Time.Format(),
	})
}

// HandleJoinAPI creates a new player and returns its ID.
func (s *Server) HandleJoinAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req models.JoinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		req.Symbol = "玩家"
	}
	if req.Symbol == "" {
		req.Symbol = "玩家"
	}

	id, pos := s.engine.AddPlayer(req.Symbol)
	writeJSON(w, http.StatusOK, models.JoinResponse{
		PlayerID: id,
		Pos:      pos,
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
