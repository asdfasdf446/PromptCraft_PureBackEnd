package tui

import (
	"encoding/json"
	"fmt"
	"log"
	"promptcraft/pkg/models"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gorilla/websocket"
)

var (
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00FF00")).
			Bold(true).
			MarginLeft(1)

	cellStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("63")).
			Width(6).
			Height(2).
			Align(lipgloss.Center, lipgloss.Center)

	playerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#4488FF")).Bold(true)
	terrainStyles = map[models.TerrainType]lipgloss.Style{
		models.TerrainGround:   lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")),
		models.TerrainObstacle: lipgloss.NewStyle().Foreground(lipgloss.Color("#FF4444")).Bold(true),
		models.TerrainSoil:     lipgloss.NewStyle().Foreground(lipgloss.Color("#AA8844")),
	}
	plantStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00AA00"))

	// --- module border colors ---
	mapBoxColor   = lipgloss.Color("#00FF00") // green
	statsBoxColor = lipgloss.Color("#4488FF") // blue
	logBoxColor   = lipgloss.Color("#FFAA00") // amber
	inputBoxColor = lipgloss.Color("#00AAFF") // cyan

	logStyle = lipgloss.NewStyle().MarginLeft(1)

	inputContainerStyle = lipgloss.NewStyle().Padding(0, 1)

	inputPrefixStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")).Bold(true)
	ghostStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	timeStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAA00")).Bold(true).MarginLeft(2)
	compassStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#00AAFF")).Bold(true)

	boxTitleStyle = lipgloss.NewStyle().Bold(true)
	titledBoxPad  = lipgloss.NewStyle().Padding(0, 1)

	statsPanelStyle = lipgloss.NewStyle().Padding(1, 2).MarginLeft(2)

	statsLabelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#AAAAAA"))
	statsHPColor     = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF4444")).Bold(true)
	statsAPColor     = lipgloss.NewStyle().Foreground(lipgloss.Color("#44AAFF")).Bold(true)
	statsSatietyColor = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF8800")).Bold(true)
	statsPosColor    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAA00")).Bold(true)

	deadStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF0000")).
			Background(lipgloss.Color("#000000")).
			Bold(true).
			Padding(2, 4).
			Border(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color("#FF0000"))
)

type stateMsg models.WorldState

type Model struct {
	input        textinput.Model
	gameState    models.WorldState
	conn         *websocket.Conn
	suggestions  []string
	history      []string
	historyIdx   int
	width        int
	height       int
	lastTabInput string
	tabCycleIdx  int
	ghost        string
}

func NewModel(addr string) Model {
	ti := textinput.New()
	ti.Placeholder = ""
	ti.Prompt = ""
	ti.Focus()
	ti.CharLimit = 64

	conn, _, err := websocket.DefaultDialer.Dial(fmt.Sprintf("ws://%s/ws", addr), nil)
	if err != nil {
		log.Fatalf("Dial error: %v", err)
	}

	return Model{
		input:       ti,
		conn:        conn,
		suggestions: []string{
			"move north", "move south", "move east", "move west",
			"eat", "eat north", "eat south", "eat east", "eat west",
			"attack", "attack north", "attack south", "attack east", "attack west",
		},
		history:     []string{},
		historyIdx:  0,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.waitForActivity())
}

func (m Model) waitForActivity() tea.Cmd {
	return func() tea.Msg {
		_, message, err := m.conn.ReadMessage()
		if err != nil {
			return nil
		}
		var state models.WorldState
		json.Unmarshal(message, &state)
		return stateMsg(state)
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.conn.Close()
			return m, tea.Quit

		case "enter":
			val := m.input.Value()
			if val != "" {
				c := models.Command{Type: "command", Payload: val}
				payload, _ := json.Marshal(c)
				m.conn.WriteMessage(websocket.TextMessage, payload)

				m.history = append(m.history, val)
				m.historyIdx = len(m.history)
				m.tabCycleIdx = 0
				m.lastTabInput = ""
				m.ghost = ""

				m.input.SetValue("")
			}

		case "up":
			if len(m.history) > 0 && m.historyIdx > 0 {
				m.historyIdx--
				m.input.SetValue(m.history[m.historyIdx])
				m.input.CursorEnd()
			}
			m.ghost = ""
			m.lastTabInput = ""
			m.tabCycleIdx = 0

		case "down":
			if m.historyIdx < len(m.history)-1 {
				m.historyIdx++
				m.input.SetValue(m.history[m.historyIdx])
				m.input.CursorEnd()
			} else {
				m.historyIdx = len(m.history)
				m.input.SetValue("")
			}
			m.ghost = ""
			m.lastTabInput = ""
			m.tabCycleIdx = 0

		case "right":
			if m.ghost != "" && m.input.Position() >= len(m.input.Value()) {
				m.input.SetValue(m.input.Value() + m.ghost)
				m.input.CursorEnd()
				m.ghost = ""
				m.lastTabInput = ""
				m.tabCycleIdx = 0
			}

		case "tab":
			m.autocomplete()
		}

	case stateMsg:
		m.gameState = models.WorldState(msg)
		return m, m.waitForActivity()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

	m.input, cmd = m.input.Update(msg)
	m.updateGhost()
	return m, cmd
}

func (m *Model) updateGhost() {
	val := strings.ToLower(m.input.Value())
	if val == "" {
		m.ghost = ""
		return
	}
	for _, s := range m.suggestions {
		if strings.HasPrefix(s, val) && s != val {
			m.ghost = s[len(val):]
			return
		}
	}
	m.ghost = ""
}

func (m *Model) autocomplete() {
	val := strings.ToLower(m.input.Value())
	if val == "" {
		return
	}

	var matches []string
	for _, s := range m.suggestions {
		if strings.HasPrefix(s, val) {
			matches = append(matches, s)
		}
	}
	if len(matches) == 0 {
		return
	}

	if m.input.Value() != m.lastTabInput {
		m.tabCycleIdx = 0
		m.lastTabInput = m.input.Value()
	}

	selected := matches[m.tabCycleIdx%len(matches)]
	m.input.SetValue(selected)
	m.input.CursorEnd()
	m.tabCycleIdx++
	m.ghost = ""
}

// ============================================================================
// Stats Panel
// ============================================================================

func (m Model) renderStatsPanel() string {
	if len(m.gameState.Players) == 0 {
		return ""
	}
	p := m.gameState.Players[0]

	hpBar := m.renderBar(p.Health, p.MaxHP, statsHPColor)
	apBar := m.renderBar(p.AP, p.MaxAP, statsAPColor)
	satietyBar := m.renderBar(p.Satiety, p.MaxSatiety, statsSatietyColor)

	lines := []string{
		statsLabelStyle.Render("HP  ") + hpBar,
		statsLabelStyle.Render("AP  ") + apBar,
		statsLabelStyle.Render("饱食") + satietyBar,
		statsLabelStyle.Render("Pos ") + statsPosColor.Render(fmt.Sprintf("(%d, %d)", p.Pos.X, p.Pos.Y)),
	}

	if p.IsDead() {
		lines = append(lines, "",
			lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Bold(true).Render("☠ 已死亡"),
		)
	}

	return statsPanelStyle.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m Model) renderBar(current, max int, color lipgloss.Style) string {
	barWidth := 10
	filled := current * barWidth / max
	if filled < 0 {
		filled = 0
	}
	empty := barWidth - filled
	if empty < 0 {
		empty = 0
	}

	filledBar := strings.Repeat("█", filled)
	emptyBar := strings.Repeat("░", empty)

	return color.Render(filledBar) +
		lipgloss.NewStyle().Foreground(lipgloss.Color("#444444")).Render(emptyBar) +
		fmt.Sprintf(" %d/%d", current, max)
}

// ============================================================================
// Helpers
// ============================================================================

// titledBox wraps content in a border with a colored title embedded in the top edge.
func titledBox(title string, borderColor, titleColor lipgloss.Color, content string) string {
	titleStr := boxTitleStyle.Foreground(titleColor).Render(" " + title + " ")
	titleW := lipgloss.Width(titleStr)

	lines := strings.Split(content, "\n")
	maxLineW := 0
	for _, line := range lines {
		w := lipgloss.Width(line)
		if w > maxLineW {
			maxLineW = w
		}
	}

	innerW := maxLineW
	if titleW > innerW {
		innerW = titleW
	}

	// Content line width: "│ " + content + pad + " │" = innerW + 4
	// Top border: "┌─" + dashes + title + dashes + "─┐" = innerW + 4
	dashTotal := innerW - titleW
	leftDash := dashTotal / 2
	rightDash := dashTotal - leftDash
	topBorder := "┌─" + strings.Repeat("─", leftDash) + titleStr + strings.Repeat("─", rightDash) + "─┐"

	// Bottom border: "└" + dashes + "┘"  => width = contentW
	bottomBorder := "└" + strings.Repeat("─", innerW+2) + "┘"

	var boxed strings.Builder
	boxed.WriteString(topBorder + "\n")
	for _, line := range lines {
		pad := innerW - lipgloss.Width(line)
		if pad < 0 {
			pad = 0
		}
		boxed.WriteString("│ " + line + strings.Repeat(" ", pad) + " │\n")
	}
	boxed.WriteString(bottomBorder)

	return lipgloss.NewStyle().Foreground(borderColor).Render(boxed.String())
}

func centerIn(s string, width int) string {
	if len(s) >= width {
		return s
	}
	left := (width - len(s)) / 2
	right := width - len(s) - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}

// plantAt returns the plant at the given position, if any.
func plantAt(plants []models.Plant, x, y int) *models.Plant {
	for i := range plants {
		if plants[i].Pos.X == x && plants[i].Pos.Y == y && plants[i].Health > 0 {
			return &plants[i]
		}
	}
	return nil
}

// ============================================================================
// View
// ============================================================================

func (m Model) View() string {
	if m.gameState.Map == nil {
		return "Connecting to server..."
	}

	// --- Death screen ---
	if len(m.gameState.Players) > 0 && m.gameState.Players[0].IsDead() {
		return m.renderDeathScreen()
	}

	// 1. World View (Map with coordinates and compass)
	const cellW = 8
	gutterW := 3

	compassStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#00AAFF")).Bold(true)

	xHeader := strings.Repeat(" ", gutterW)
	for x := 0; x < models.MapSize; x++ {
		xHeader += centerIn(fmt.Sprintf("%d", x), cellW)
	}
	xHeader = compassStyle.Render("W ← ") + xHeader + compassStyle.Render(" E →")

	mapFullW := gutterW + models.MapSize*cellW + 10

	northLine := lipgloss.PlaceHorizontal(mapFullW, lipgloss.Center,
		compassStyle.Render("N ↑"),
	)

	var mapRows []string
	for y := 0; y < models.MapSize; y++ {
		var rowCells []string
		for x := 0; x < models.MapSize; x++ {
			cell := m.gameState.Map[y][x]

			// Entities in cell: plants + players
			var entitiesInCell []string

			// Plants (from Plants slice)
			if p := plantAt(m.gameState.Plants, x, y); p != nil {
				entitiesInCell = append(entitiesInCell, plantStyle.Render(p.Symbol))
			}

			// Players
			for _, p := range m.gameState.Players {
				if p.Pos.X == x && p.Pos.Y == y {
					entitiesInCell = append(entitiesInCell, playerStyle.Render(p.Symbol))
				}
			}

			ts, ok := terrainStyles[cell.Terrain.Type]
			if !ok {
				ts = lipgloss.NewStyle()
			}
			content := ts.Render(cell.Terrain.Name())

			// Also render remaining entities from Cell.Entities (non-unit decorations)
			for _, ent := range cell.Entities {
				entitiesInCell = append(entitiesInCell, ent)
			}

			if len(entitiesInCell) > 0 {
				content = content + "\n" + strings.Join(entitiesInCell, "\n")
			}

			rowCells = append(rowCells, cellStyle.Render(content))
		}
		yLabel := compassStyle.Render(fmt.Sprintf("%2d", y))
		allCells := append([]string{yLabel}, rowCells...)
		mapRows = append(mapRows, lipgloss.JoinHorizontal(lipgloss.Top, allCells...))
	}

	southLine := lipgloss.PlaceHorizontal(mapFullW, lipgloss.Center,
		compassStyle.Render("S ↓"),
	)

	mapBody := lipgloss.JoinVertical(lipgloss.Left,
		northLine,
		xHeader,
		lipgloss.JoinVertical(lipgloss.Left, mapRows...),
		southLine,
	)

	header := lipgloss.JoinHorizontal(lipgloss.Center,
		titleStyle.Render("PROMPT CRAFT - WORLD VIEW"),
		timeStyle.Render(m.gameState.Time.Format()),
	)

	mapBox := titledBox("地图", mapBoxColor, mapBoxColor,
		lipgloss.NewStyle().Padding(1).Render(mapBody),
	)
	statsBox := titledBox("状态", statsBoxColor, statsBoxColor, m.renderStatsPanel())
	mapAndStats := lipgloss.JoinHorizontal(lipgloss.Top, mapBox, statsBox)
	worldView := header + "\n" + mapAndStats

	// 2. Logs View
	var logBuilder strings.Builder
	logDisplayCount := 5
	start := 0
	if len(m.gameState.Logs) > logDisplayCount {
		start = len(m.gameState.Logs) - logDisplayCount
	}
	for i := start; i < len(m.gameState.Logs); i++ {
		logBuilder.WriteString(m.gameState.Logs[i] + "\n")
	}

	logsContent := logBuilder.String()
	if logsContent == "" {
		logsContent = " "
	}
	logsView := titledBox("日志", logBoxColor, logBoxColor,
		logStyle.Width(m.width-8).Render(logsContent),
	)

	// 3. Input View
	inputPrefix := inputPrefixStyle.Render("> ")
	m.input.Width = max(len(m.input.Value())+2, 1)
	inputLine := lipgloss.JoinHorizontal(lipgloss.Top,
		inputPrefix,
		m.input.View(),
		ghostStyle.Render(m.ghost),
	)
	inputBody := inputContainerStyle.Width(m.width - 4).Render(inputLine)
	inputView := titledBox("指令", inputBoxColor, inputBoxColor, inputBody)

	mainContent := lipgloss.JoinVertical(lipgloss.Left, worldView, logsView)

	contentHeight := lipgloss.Height(mainContent)
	inputHeight := lipgloss.Height(inputView)
	fillerHeight := m.height - contentHeight - inputHeight
	if fillerHeight < 0 {
		fillerHeight = 0
	}
	filler := strings.Repeat("\n", fillerHeight)

	return mainContent + filler + inputView
}

func (m Model) renderDeathScreen() string {
	msg := deadStyle.Render("☠ 你 已 死 亡 ☠") + "\n\n" +
		"按 Ctrl+C 退出游戏"

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		msg,
	)
}
