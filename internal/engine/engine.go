package engine

import (
	"fmt"
	"math/rand"
	"promptcraft/pkg/models"
	"strings"
	"sync"
	"time"
)

const (
	ActionCostMove = 1
	ActionCostEat  = 1
	ActionCostAttack = 3

	AttackDamage = 3 // damage per successful attack

	SatietyPerEat     = 2 // satiety restored per plant eaten
	SatietyTickSecs   = 30
	SatietyHealThresh = 0.5 // heal when satiety > 50%
)

type GameEngine struct {
	mu         sync.RWMutex
	state      models.WorldState
	gameTime   *models.GameTime
	stopCh     chan struct{}
	rng        *rand.Rand
	plantSeq   int // auto-increment for plant IDs
}

func NewGameEngine() *GameEngine {
	gameTime := models.NewGameTime()
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	e := &GameEngine{
		state: models.WorldState{
			Players: []models.Player{
				models.NewPlayer("p1", models.Point{X: 5, Y: 5}),
			},
			Plants: []models.Plant{},
			Logs:   []string{"Welcome to PromptCraft!"},
			Time:   gameTime,
		},
		gameTime: gameTime,
		rng:      rng,
	}

	e.state.Map = e.generateMap()
	return e
}

// ============================================================================
// Map Generation
// ============================================================================

func (e *GameEngine) generateMap() [][]models.Cell {
	size := models.MapSize
	grid := make([][]models.Cell, size)
	for i := range grid {
		grid[i] = make([]models.Cell, size)
		for j := range grid[i] {
			grid[i][j] = models.Cell{
				Terrain:  models.NewTerrain(models.TerrainGround),
				Entities: []string{},
			}
		}
	}

	e.placeObstacles(grid)
	e.placeSoil(grid)
	return grid
}

func (e *GameEngine) placeObstacles(grid [][]models.Cell) {
	size := models.MapSize
	target := size * size * 10 / 100
	placed := 0
	maxAttempts := target * 20

	for placed < target && maxAttempts > 0 {
		maxAttempts--
		x := e.rng.Intn(size)
		y := e.rng.Intn(size)

		if x == 5 && y == 5 {
			continue
		}
		if grid[y][x].Terrain.Type != models.TerrainGround {
			continue
		}
		if e.hasObstacleNeighbor(grid, x, y) {
			continue
		}

		grid[y][x].Terrain = models.NewTerrain(models.TerrainObstacle)
		placed++
	}
}

func (e *GameEngine) hasObstacleNeighbor(grid [][]models.Cell, x, y int) bool {
	size := models.MapSize
	dirs := [][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}
	for _, d := range dirs {
		nx, ny := x+d[0], y+d[1]
		if nx >= 0 && nx < size && ny >= 0 && ny < size {
			if grid[ny][nx].Terrain.Type == models.TerrainObstacle {
				return true
			}
		}
	}
	return false
}

func (e *GameEngine) placeSoil(grid [][]models.Cell) {
	size := models.MapSize
	target := size * size * 20 / 100
	placed := 0
	maxAttempts := target * 20

	for placed < target && maxAttempts > 0 {
		maxAttempts--
		x := e.rng.Intn(size)
		y := e.rng.Intn(size)

		if x == 5 && y == 5 {
			continue
		}
		if grid[y][x].Terrain.Type != models.TerrainGround {
			continue
		}

		grid[y][x].Terrain = models.NewTerrain(models.TerrainSoil)
		placed++
	}
}

// ============================================================================
// State Access
// ============================================================================

func (e *GameEngine) GetState() models.WorldState {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.state
}

func (e *GameEngine) findPlayer(id string) *models.Player {
	for i := range e.state.Players {
		if e.state.Players[i].ID == id {
			return &e.state.Players[i]
		}
	}
	return nil
}

// AddPlayer creates a new player at a random walkable position and returns its ID and spawn.
func (e *GameEngine) AddPlayer(symbol string) (string, models.Point) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Find a random walkable, unoccupied spot
	size := models.MapSize
	for attempt := 0; attempt < 100; attempt++ {
		x := e.rng.Intn(size)
		y := e.rng.Intn(size)
		if !e.state.Map[y][x].Terrain.IsWalkable() {
			continue
		}
		occupied := false
		for _, p := range e.state.Players {
			if p.Pos.X == x && p.Pos.Y == y {
				occupied = true
				break
			}
		}
		if occupied {
			continue
		}
		id := fmt.Sprintf("p%d", len(e.state.Players)+1)
		e.state.Players = append(e.state.Players, models.NewPlayer(id, models.Point{X: x, Y: y}))
		e.state.Players[len(e.state.Players)-1].Symbol = symbol
		return id, models.Point{X: x, Y: y}
	}

	// Fallback: place at a hardcoded position
	id := fmt.Sprintf("p%d", len(e.state.Players)+1)
	pos := models.Point{X: 0, Y: 0}
	e.state.Players = append(e.state.Players, models.NewPlayer(id, pos))
	e.state.Players[len(e.state.Players)-1].Symbol = symbol
	return id, pos
}

// ============================================================================
// Command Processing
// ============================================================================

func (e *GameEngine) ProcessCommand(rawCmd string) (string, bool) {
	return e.ProcessCommandForPlayer("p1", rawCmd)
}

func (e *GameEngine) ProcessCommandForPlayer(playerID, rawCmd string) (string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	player := e.findPlayer(playerID)
	if player == nil {
		return "玩家不存在", false
	}
	if player.IsDead() {
		return "你已经死了，请重新开始游戏", false
	}

	parts := strings.Fields(strings.ToLower(rawCmd))
	if len(parts) == 0 {
		return "Empty command", false
	}

	action := parts[0]
	var msg string
	var success bool

	switch action {
	case "move":
		if len(parts) < 2 {
			return "Move where? (north, south, east, west)", false
		}
		if !e.consumeAP(player, ActionCostMove) {
			return fmt.Sprintf("行动点不足! (需要 %d, 当前 %d)", ActionCostMove, player.AP), false
		}
		msg, success = e.handleMove(player, parts[1])

	case "eat":
		if !e.consumeAP(player, ActionCostEat) {
			return fmt.Sprintf("行动点不足! (需要 %d, 当前 %d)", ActionCostEat, player.AP), false
		}
		var target models.Point
		if len(parts) < 2 {
			target = player.Pos // eat from current tile
		} else {
			target = e.resolveDirection(player.Pos, parts[1])
		}
		msg, success = e.handleEat(player, target)

	case "attack":
		if !e.consumeAP(player, ActionCostAttack) {
			return fmt.Sprintf("行动点不足! (需要 %d, 当前 %d)", ActionCostAttack, player.AP), false
		}
		var target models.Point
		if len(parts) < 2 {
			target = player.Pos
		} else {
			target = e.resolveDirection(player.Pos, parts[1])
		}
		msg, success = e.handleAttack(player, target)

	default:
		msg = fmt.Sprintf("未知指令: %s", action)
		success = false
	}

	e.state.Logs = append(e.state.Logs, msg)
	if len(e.state.Logs) > 100 {
		e.state.Logs = e.state.Logs[len(e.state.Logs)-100:]
	}
	return msg, success
}

func (e *GameEngine) consumeAP(p *models.Player, cost int) bool {
	if p.AP < cost {
		return false
	}
	p.AP -= cost
	return true
}

// resolveDirection returns the adjacent point in the given direction.
// Does NOT check bounds — callers must validate.
func (e *GameEngine) resolveDirection(from models.Point, dir string) models.Point {
	switch dir {
	case "north":
		from.Y--
	case "south":
		from.Y++
	case "east":
		from.X++
	case "west":
		from.X--
	}
	return from
}

// ============================================================================
// Move
// ============================================================================

func (e *GameEngine) handleMove(p *models.Player, dir string) (string, bool) {
	newPos := e.resolveDirection(p.Pos, dir)

	// Detect invalid direction (resolveDirection returned same position)
	if newPos == p.Pos && dir != "" {
		// short directions like "north" etc will always change pos; unknown dir won't
	}

	if newPos.X < 0 || newPos.X >= models.MapSize || newPos.Y < 0 || newPos.Y >= models.MapSize {
		return "你撞到世界边界了!", false
	}

	targetCell := e.state.Map[newPos.Y][newPos.X]
	if !targetCell.Terrain.IsWalkable() {
		return "撞到障碍了", false
	}

	p.Pos = newPos
	return fmt.Sprintf("向%s移动到了 (%d, %d)", dir, p.Pos.X, p.Pos.Y), true
}

// ============================================================================
// Eat
// ============================================================================

func (e *GameEngine) handleEat(p *models.Player, target models.Point) (string, bool) {
	if target.X < 0 || target.X >= models.MapSize || target.Y < 0 || target.Y >= models.MapSize {
		return "目标位置超出地图边界", false
	}

	for i := range e.state.Plants {
		plant := &e.state.Plants[i]
		if plant.Pos == target && plant.Health > 0 {
			plant.Health--
			if plant.Health <= 0 {
				plant.Health = 0
				// Remove dead plants from the slice
				e.state.Plants = append(e.state.Plants[:i], e.state.Plants[i+1:]...)
			}

			before := p.Satiety
			p.Satiety += SatietyPerEat
			if p.Satiety > p.MaxSatiety {
				p.Satiety = p.MaxSatiety
			}
			restored := p.Satiety - before

			return fmt.Sprintf("你吃掉了植物, 饱食度恢复 %d (当前 %d/%d, AP -%d)",
				restored, p.Satiety, p.MaxSatiety, ActionCostEat), true
		}
	}

	return fmt.Sprintf("这里没有植物可以吃 (消耗了 %d 行动点)", ActionCostEat), false
}

// ============================================================================
// Attack
// ============================================================================

func (e *GameEngine) handleAttack(attacker *models.Player, target models.Point) (string, bool) {
	if target.X < 0 || target.X >= models.MapSize || target.Y < 0 || target.Y >= models.MapSize {
		return "目标位置超出地图边界", false
	}

	for i := range e.state.Players {
		defender := &e.state.Players[i]
		if defender.ID == attacker.ID {
			continue // can't attack self
		}
		if defender.Pos == target && !defender.IsDead() {
			defender.Health -= AttackDamage
			if defender.Health < 0 {
				defender.Health = 0
			}

			attackerMsg := fmt.Sprintf("你攻击了 %s, 造成 %d 点伤害! (消耗 %d 行动点)",
				defender.Symbol, AttackDamage, ActionCostAttack)

			// Also log for the defender
			defenderMsg := fmt.Sprintf("你被 %s 攻击了, 受到 %d 点伤害!",
				attacker.Symbol, AttackDamage)
			e.state.Logs = append(e.state.Logs, defenderMsg)

			if defender.IsDead() {
				e.state.Logs = append(e.state.Logs,
					fmt.Sprintf("%s 被击杀了!", defender.Symbol))
			}

			return attackerMsg, true
		}
	}

	return fmt.Sprintf("这里没有可以攻击的目标 (消耗了 %d 行动点)", ActionCostAttack), false
}

// ============================================================================
// Plant Growth (in time tick)
// ============================================================================

func (e *GameEngine) growPlants() {
	size := models.MapSize
	// ~5% chance per tick to attempt growth
	if e.rng.Intn(100) < 5 {
		for attempt := 0; attempt < 5; attempt++ {
			x := e.rng.Intn(size)
			y := e.rng.Intn(size)
			if e.state.Map[y][x].Terrain.Type != models.TerrainSoil {
				continue
			}
			// Don't grow where a plant already exists
			occupied := false
			for _, plant := range e.state.Plants {
				if plant.Pos.X == x && plant.Pos.Y == y && plant.Health > 0 {
					occupied = true
					break
				}
			}
			if occupied {
				continue
			}

			e.plantSeq++
			e.state.Plants = append(e.state.Plants,
				models.NewPlant(fmt.Sprintf("plant_%d", e.plantSeq), models.Point{X: x, Y: y}),
			)
			break
		}
	}
}

// ============================================================================
// Time System
// ============================================================================

func (e *GameEngine) StartTime(onTick func()) {
	e.stopCh = make(chan struct{})
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				e.mu.Lock()
				e.gameTime.Advance()
				e.state.Time = e.gameTime
				e.regenerateAP()
				e.growPlants()

				sec := e.gameTime.TotalSeconds()
				if sec > 0 && sec%SatietyTickSecs == 0 {
					e.tickSatiety()
				}

				e.mu.Unlock()
				if onTick != nil {
					onTick()
				}
			case <-e.stopCh:
				return
			}
		}
	}()
}

func (e *GameEngine) regenerateAP() {
	for i := range e.state.Players {
		p := &e.state.Players[i]
		if p.IsDead() {
			continue
		}
		if p.AP < p.MaxAP {
			p.AP++
		}
	}
}

func (e *GameEngine) tickSatiety() {
	for i := range e.state.Players {
		p := &e.state.Players[i]
		if p.IsDead() {
			continue
		}

		// Satiety decreases by 1 every tick
		p.Satiety--
		if p.Satiety < 0 {
			p.Satiety = 0
		}

		// If satiety is 0, lose 1 HP
		if p.Satiety == 0 {
			p.Health--
			e.state.Logs = append(e.state.Logs,
				"饥饿导致你失去了 1 点生命值!")
		}

		// If satiety > 50% and HP not full, recover 1 HP
		if float64(p.Satiety) > float64(p.MaxSatiety)*SatietyHealThresh && p.Health < p.MaxHP {
			p.Health++
			e.state.Logs = append(e.state.Logs,
				"饱食状态良好, 恢复了 1 点生命值")
		}

		// Death check
		if p.Health <= 0 {
			p.Health = 0
			e.state.Logs = append(e.state.Logs, "你已经死了!")
		}
	}
}

func (e *GameEngine) StopTime() {
	if e.stopCh != nil {
		close(e.stopCh)
	}
}
