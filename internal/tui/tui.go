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
	titleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00")).Bold(true).Padding(0, 1)
	mapStyle   = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Padding(0).BorderForeground(lipgloss.Color("63"))
	logStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("63")).Width(60).Height(10)
	inputStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF"))
)

type stateMsg models.WorldState

type Model struct {
	input       textinput.Model
	gameState   models.WorldState
	conn        *websocket.Conn
	suggestions []string
	width       int
	height      int
}

func NewModel(addr string) Model {
	ti := textinput.New()
	ti.Placeholder = "Type command (e.g., move north)..."
	ti.Focus()
	ti.CharLimit = 64

	conn, _, err := websocket.DefaultDialer.Dial(fmt.Sprintf("ws://%s/ws", addr), nil)
	if err != nil {
		log.Fatalf("Dial error: %v", err)
	}

	return Model{
		input:       ti,
		conn:        conn,
		suggestions: []string{"move north", "move south", "move east", "move west", "attack"},
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
				m.input.SetValue("")
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
	return m, cmd
}

func (m *Model) autocomplete() {
	val := strings.ToLower(m.input.Value())
	if val == "" {
		return
	}
	for _, s := range m.suggestions {
		if strings.HasPrefix(s, val) {
			m.input.SetValue(s)
			return
		}
	}
}

func (m Model) View() string {
	if m.gameState.Map == nil {
		return "Connecting to server..."
	}

	// Render Map
	var mapBuilder strings.Builder
	// Create a copy of the map to render players
	renderMap := make([][]string, models.MapSize)
	for i := range m.gameState.Map {
		renderMap[i] = make([]string, models.MapSize)
		copy(renderMap[i], m.gameState.Map[i])
	}
	for _, p := range m.gameState.Players {
		if p.Pos.Y >= 0 && p.Pos.Y < models.MapSize && p.Pos.X >= 0 && p.Pos.X < models.MapSize {
			renderMap[p.Pos.Y][p.Pos.X] = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Render(p.Symbol)
		}
	}

	for _, row := range renderMap {
		mapBuilder.WriteString(strings.Join(row, " ") + "\n")
	}

	viewArea := titleStyle.Render("PROMPT CRAFT - WORLD VIEW") + "\n" + mapStyle.Render(mapBuilder.String())

	// Render Logs
	var logBuilder strings.Builder
	start := 0
	if len(m.gameState.Logs) > 10 {
		start = len(m.gameState.Logs) - 10
	}
	for i := start; i < len(m.gameState.Logs); i++ {
		logBuilder.WriteString(m.gameState.Logs[i] + "\n")
	}
	logArea := logStyle.Render(logBuilder.String())

	// Input
	inputArea := "\n" + inputStyle.Render(" COMMAND: ") + m.input.View()

	return lipgloss.JoinVertical(lipgloss.Left, viewArea, logArea, inputArea)
}
