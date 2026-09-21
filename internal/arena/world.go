package arena

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
)

type terrain uint8

const (
	ground terrain = iota
	soil
	obstacle
)

type cell struct {
	terrain terrain
	food    bool
}

type World struct {
	config       Config
	rng          *rand.Rand
	cells        [][]cell
	players      map[string]*Player
	order        []string
	virtualMS    int64
	nextHungerMS int64
	nextEnergyMS int64
	nextFoodMS   int64
	foodSeq      int
}

type WorldEvent struct {
	VirtualMS int64
	Type      string
	ActorID   string
	Data      any
}

func NewWorld(config Config) (*World, error) {
	w := &World{
		config:       config,
		rng:          rand.New(rand.NewSource(config.Seed)),
		players:      make(map[string]*Player),
		nextHungerMS: config.HungerTickMS,
		nextEnergyMS: 1_000,
		nextFoodMS:   config.FoodSpawnMS,
	}
	w.cells = make([][]cell, config.Height)
	for y := range w.cells {
		w.cells[y] = make([]cell, config.Width)
	}
	w.generateTerrain()
	if err := w.spawnPlayers(); err != nil {
		return nil, err
	}
	initialFood := max(4, len(config.Agents)*2)
	for range initialFood {
		w.spawnFood()
	}
	return w, nil
}

func (w *World) generateTerrain() {
	total := w.config.Width * w.config.Height
	place := func(count int, kind terrain) {
		for placed, attempts := 0, 0; placed < count && attempts < total*20; attempts++ {
			x, y := w.rng.Intn(w.config.Width), w.rng.Intn(w.config.Height)
			if w.cells[y][x].terrain != ground {
				continue
			}
			if kind == obstacle && w.hasObstacleNeighbor(Point{X: x, Y: y}) {
				continue
			}
			w.cells[y][x].terrain = kind
			placed++
		}
	}
	place(total/10, obstacle)
	place(total/4, soil)
}

func (w *World) hasObstacleNeighbor(p Point) bool {
	for _, d := range []Point{{X: 0, Y: -1}, {X: 0, Y: 1}, {X: -1, Y: 0}, {X: 1, Y: 0}} {
		n := Point{X: p.X + d.X, Y: p.Y + d.Y}
		if w.inBounds(n) && w.cells[n.Y][n.X].terrain == obstacle {
			return true
		}
	}
	return false
}

func (w *World) spawnPlayers() error {
	for _, agent := range w.config.Agents {
		var pos Point
		found := false
		for attempts := 0; attempts < w.config.Width*w.config.Height*4; attempts++ {
			candidate := Point{X: w.rng.Intn(w.config.Width), Y: w.rng.Intn(w.config.Height)}
			if w.walkable(candidate) && w.playerAt(candidate, "") == nil {
				pos, found = candidate, true
				break
			}
		}
		if !found {
			return fmt.Errorf("could not find spawn for %q", agent.ID)
		}
		w.players[agent.ID] = &Player{
			ID:            agent.ID,
			Pos:           pos,
			HP:            w.config.StartingHP,
			Hunger:        w.config.StartingHunger,
			Energy:        w.config.StartingEnergy,
			BudgetNanoUSD: USDToNano(w.config.StartingBudgetUSD),
			Alive:         true,
		}
		w.order = append(w.order, agent.ID)
	}
	return nil
}

func (w *World) Advance(toMS int64) []WorldEvent {
	if toMS < w.virtualMS {
		panic("arena: virtual time moved backwards")
	}
	var events []WorldEvent
	for {
		next := min(w.nextHungerMS, min(w.nextEnergyMS, w.nextFoodMS))
		if next > toMS {
			break
		}
		w.virtualMS = next
		if next == w.nextHungerMS {
			for _, id := range w.order {
				p := w.players[id]
				if !p.Alive {
					continue
				}
				p.Hunger--
				if p.Hunger < 0 {
					p.Hunger = 0
				}
				if p.Hunger == 0 {
					p.HP--
					if p.HP <= 0 {
						events = append(events, w.kill(p, "starvation"))
					}
				}
			}
			w.nextHungerMS += w.config.HungerTickMS
		}
		if next == w.nextEnergyMS {
			for _, p := range w.players {
				if p.Alive && p.Energy < w.config.StartingEnergy {
					p.Energy++
				}
			}
			w.nextEnergyMS += 1_000
		}
		if next == w.nextFoodMS {
			if pos, ok := w.spawnFood(); ok {
				events = append(events, WorldEvent{VirtualMS: w.virtualMS, Type: "food_spawned", Data: map[string]any{"position": pos}})
			}
			w.nextFoodMS += w.config.FoodSpawnMS
		}
		if w.AliveCount() <= 1 {
			return events
		}
	}
	w.virtualMS = toMS
	return events
}

func (w *World) Charge(agentID string, cost int64, receipt Receipt) (bool, []WorldEvent) {
	p := w.players[agentID]
	if p == nil || !p.Alive {
		return false, nil
	}
	p.Decisions++
	p.TotalLatencyMS += receipt.LatencyMS
	p.InputTokens += receipt.InputTokens
	p.OutputTokens += receipt.OutputTokens
	p.SpentNanoUSD += cost
	p.BudgetNanoUSD -= cost
	if p.BudgetNanoUSD <= 0 {
		p.BudgetNanoUSD = 0
		return false, []WorldEvent{w.kill(p, "budget_exhausted")}
	}
	return true, nil
}

func (w *World) Apply(agentID, action string) (bool, string, []WorldEvent) {
	p := w.players[agentID]
	if p == nil || !p.Alive {
		return false, "actor is not alive", nil
	}
	p.Defending = false
	parts := strings.Split(action, "_")
	verb := parts[0]
	direction := ""
	if len(parts) > 1 {
		direction = parts[1]
	}
	var success bool
	var message string
	var events []WorldEvent
	switch verb {
	case "move":
		if !w.consumeEnergy(p, 1) {
			message = "not enough energy"
			break
		}
		target, ok := w.direction(p.Pos, direction)
		if !ok || !w.walkable(target) || w.playerAt(target, "") != nil {
			message = "movement blocked"
			break
		}
		p.Pos = target
		success, message = true, "moved"
	case "eat":
		if !w.consumeEnergy(p, 1) {
			message = "not enough energy"
			break
		}
		target := p.Pos
		var ok bool
		if direction != "here" {
			target, ok = w.direction(p.Pos, direction)
			if !ok {
				message = "invalid direction"
				break
			}
		}
		if !w.inBounds(target) || !w.cells[target.Y][target.X].food {
			message = "no food at target"
			break
		}
		w.cells[target.Y][target.X].food = false
		p.Hunger = min(w.config.StartingHunger, p.Hunger+4)
		success, message = true, "ate food"
	case "attack":
		if !w.consumeEnergy(p, 3) {
			message = "not enough energy"
			break
		}
		targetPos, ok := w.direction(p.Pos, direction)
		if !ok {
			message = "invalid direction"
			break
		}
		target := w.playerAt(targetPos, p.ID)
		if target == nil || !target.Alive {
			message = "no live target"
			break
		}
		damage := 3
		if target.Defending {
			damage = 1
			target.Defending = false
		}
		target.HP -= damage
		success, message = true, fmt.Sprintf("hit %s for %d", target.ID, damage)
		if target.HP <= 0 {
			events = append(events, w.kill(target, "combat:"+p.ID))
		}
	case "defend":
		if !w.consumeEnergy(p, 1) {
			message = "not enough energy"
			break
		}
		p.Defending = true
		success, message = true, "defending"
	case "wait":
		success, message = true, "waited"
	default:
		message = "invalid action"
	}
	if success {
		p.SuccessfulActions++
	}
	return success, message, events
}

func (w *World) Observation(agentID string) Observation {
	players := make([]Player, 0, len(w.order))
	for _, id := range w.order {
		players = append(players, *w.players[id])
	}
	rows := make([]string, w.config.Height)
	food := make([]Point, 0)
	for y := 0; y < w.config.Height; y++ {
		var row strings.Builder
		for x := 0; x < w.config.Width; x++ {
			pos := Point{X: x, Y: y}
			if w.cells[y][x].food {
				food = append(food, pos)
			}
			if p := w.playerAt(pos, ""); p != nil && p.Alive {
				if p.ID == agentID {
					row.WriteByte('@')
				} else {
					row.WriteByte('A')
				}
				continue
			}
			switch {
			case w.cells[y][x].food:
				row.WriteByte('F')
			case w.cells[y][x].terrain == obstacle:
				row.WriteByte('#')
			case w.cells[y][x].terrain == soil:
				row.WriteByte(':')
			default:
				row.WriteByte('.')
			}
		}
		rows[y] = row.String()
	}
	return Observation{
		VirtualMS: w.virtualMS,
		Self:      *w.players[agentID],
		Players:   players,
		MapRows:   rows,
		Food:      food,
		Legend: map[string]string{
			"@": "self", "A": "other living agent", "F": "food", "#": "obstacle", ":": "soil", ".": "ground",
		},
		LegalActions: append([]string(nil), legalActions...),
	}
}

func (w *World) AliveCount() int {
	count := 0
	for _, p := range w.players {
		if p.Alive {
			count++
		}
	}
	return count
}

func (w *World) VirtualMS() int64 { return w.virtualMS }

func (w *World) Player(id string) *Player { return w.players[id] }

func (w *World) Players() []Player {
	result := make([]Player, 0, len(w.order))
	for _, id := range w.order {
		result = append(result, *w.players[id])
	}
	return result
}

func (w *World) Snapshot() Snapshot {
	rows := make([]string, w.config.Height)
	for y := 0; y < w.config.Height; y++ {
		var row strings.Builder
		for x := 0; x < w.config.Width; x++ {
			switch {
			case w.cells[y][x].food:
				row.WriteByte('F')
			case w.cells[y][x].terrain == obstacle:
				row.WriteByte('#')
			case w.cells[y][x].terrain == soil:
				row.WriteByte(':')
			default:
				row.WriteByte('.')
			}
		}
		rows[y] = row.String()
	}
	return Snapshot{
		VirtualMS: w.virtualMS,
		MapRows:   rows,
		Players:   w.Players(),
		MaxHP:     w.config.StartingHP,
		MaxHunger: w.config.StartingHunger,
		MaxEnergy: w.config.StartingEnergy,
	}
}

func (w *World) Finalize(atMS int64) {
	for _, p := range w.players {
		if p.Alive {
			p.SurvivalMS = atMS
		}
	}
}

func (w *World) kill(p *Player, cause string) WorldEvent {
	p.Alive = false
	p.HP = max(0, p.HP)
	p.DeathCause = cause
	p.SurvivalMS = w.virtualMS
	return WorldEvent{VirtualMS: w.virtualMS, Type: "agent_died", ActorID: p.ID, Data: map[string]any{"cause": cause}}
}

func (w *World) spawnFood() (Point, bool) {
	candidates := make([]Point, 0)
	for y := range w.cells {
		for x := range w.cells[y] {
			p := Point{X: x, Y: y}
			if w.cells[y][x].terrain == soil && !w.cells[y][x].food && w.playerAt(p, "") == nil {
				candidates = append(candidates, p)
			}
		}
	}
	if len(candidates) == 0 {
		return Point{}, false
	}
	pos := candidates[w.rng.Intn(len(candidates))]
	w.cells[pos.Y][pos.X].food = true
	w.foodSeq++
	return pos, true
}

func (w *World) playerAt(pos Point, exclude string) *Player {
	for _, id := range w.order {
		p := w.players[id]
		if p.ID != exclude && p.Alive && p.Pos == pos {
			return p
		}
	}
	return nil
}

func (w *World) walkable(p Point) bool { return w.inBounds(p) && w.cells[p.Y][p.X].terrain != obstacle }
func (w *World) inBounds(p Point) bool {
	return p.X >= 0 && p.X < w.config.Width && p.Y >= 0 && p.Y < w.config.Height
}

func (w *World) direction(from Point, direction string) (Point, bool) {
	switch direction {
	case "north":
		from.Y--
	case "south":
		from.Y++
	case "east":
		from.X++
	case "west":
		from.X--
	default:
		return from, false
	}
	return from, true
}

func (w *World) consumeEnergy(p *Player, cost int) bool {
	if p.Energy < cost {
		return false
	}
	p.Energy -= cost
	return true
}

func sortedPlayers(players []Player) []Player {
	sort.SliceStable(players, func(i, j int) bool {
		if players[i].SurvivalMS != players[j].SurvivalMS {
			return players[i].SurvivalMS > players[j].SurvivalMS
		}
		if players[i].Alive != players[j].Alive {
			return players[i].Alive
		}
		if players[i].BudgetNanoUSD != players[j].BudgetNanoUSD {
			return players[i].BudgetNanoUSD > players[j].BudgetNanoUSD
		}
		if players[i].HP != players[j].HP {
			return players[i].HP > players[j].HP
		}
		return players[i].ID < players[j].ID
	})
	return players
}
