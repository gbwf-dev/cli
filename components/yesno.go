package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// YesNo is a simple yes/no prompt model.
type YesNo struct {
	input  textinput.Model
	result bool
	done   bool
}

// NewYesNo creates a new YesNo prompt with the given question.
func NewYesNo(question string) *YesNo {
	ti := textinput.New()
	ti.Focus()
	ti.Prompt = fmt.Sprintf("%s [Y/n] ", question)
	ti.CharLimit = 3
	ti.Width = 5

	return &YesNo{
		input: ti,
	}
}

// GetResult returns the result after the prompt is finished.
func (m *YesNo) GetResult() bool { return m.result }

// Init implements tea.Model
func (m *YesNo) Init() tea.Cmd { return textinput.Blink }

// Update handles user input.
func (m *YesNo) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.done {
		return m, tea.Quit
	}

	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			value := strings.ToLower(strings.TrimSpace(m.input.Value()))
			m.result = value == "y" || value == "yes"
			m.done = true
			return m, tea.Quit
		case tea.KeyCtrlC, tea.KeyEsc:
			m.result = false
			m.done = true
			return m, tea.Quit
		}
	}

	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m YesNo) View() string { return fmt.Sprintln(m.input.View()) }
