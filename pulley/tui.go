package pulley

import (
	"errors"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

type menuItem struct{ title string }

func (m menuItem) FilterValue() string { return m.title }
func (m menuItem) Title() string       { return m.title }
func (m menuItem) Description() string { return "" }

type menuModel struct {
	list      list.Model
	selection string
	action    string
}

func (m menuModel) Init() tea.Cmd {
	return func() tea.Msg {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}}
	}
}

func (m menuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.list.FilterState() == list.Filtering {
			// Delegate all key handling to the list component while filtering is active.
			break
		}
		switch msg.String() {
		case "enter":
			if it, ok := m.list.SelectedItem().(menuItem); ok {
				m.selection = it.title
				m.action = "select"
				return m, tea.Quit
			}
		case "x":
			if it, ok := m.list.SelectedItem().(menuItem); ok {
				m.selection = it.title
				m.action = "delete"
				return m, tea.Quit
			}
		case "q", "ctrl+c", "esc":
			m.action = "cancel"
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m menuModel) View() string {
	return m.list.View()
}

func pickFromList(title string, options []string, canDelete bool) (string, error) {
	selected, _, err := runMenu(title, options, canDelete)
	return selected, err
}

func pickWithDelete(title string, options []string) (string, string, error) {
	return runMenu(title, options, true)
}

func runMenu(title string, options []string, canDelete bool) (string, string, error) {
	items := make([]list.Item, 0, len(options))
	for _, opt := range options {
		items = append(items, menuItem{title: opt})
	}
	l := list.New(items, newMenuDelegate(), 60, 14)
	l.Title = title
	if canDelete {
		l.Title = title + " (x to delete)"
	}
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	model := menuModel{list: l}
	finalModel, err := tea.NewProgram(model).Run()
	if err != nil {
		return "", "", err
	}
	result, ok := finalModel.(menuModel)
	if !ok || result.action == "cancel" || result.selection == "" {
		return "", "", errors.New("selection cancelled")
	}
	return result.selection, result.action, nil
}

func newMenuDelegate() list.DefaultDelegate {
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetSpacing(0)
	return delegate
}
