package pulley

import (
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

func TestNewMenuDelegateIsFlush(t *testing.T) {
	delegate := newMenuDelegate()
	if delegate.ShowDescription {
		t.Fatal("expected descriptions to be hidden")
	}
	if spacing := delegate.Spacing(); spacing != 0 {
		t.Fatalf("expected spacing 0, got %d", spacing)
	}
}

func TestMenuModelInitStartsFiltering(t *testing.T) {
	model := menuModel{}
	cmd := model.Init()
	if cmd == nil {
		t.Fatal("expected init command")
	}

	msg := cmd()
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		t.Fatalf("expected tea.KeyMsg, got %T", msg)
	}
	if key.String() != "/" {
		t.Fatalf("expected '/' key msg, got %q", key.String())
	}
}

func TestMenuModelEnterSelectsWhileFiltering(t *testing.T) {
	items := []list.Item{menuItem{title: "repo"}}
	l := list.New(items, newMenuDelegate(), 60, 14)
	l.SetFilteringEnabled(true)

	model := menuModel{list: l}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	filteringModel := updated.(menuModel)
	if filteringModel.list.FilterState() != list.Filtering {
		t.Fatalf("expected filtering state, got %v", filteringModel.list.FilterState())
	}

	updated, _ = filteringModel.Update(tea.KeyMsg{Type: tea.KeyEnter})
	enterModel := updated.(menuModel)
	if enterModel.action != "select" || enterModel.selection != "repo" {
		t.Fatalf("expected selection while filtering, got action=%q selection=%q", enterModel.action, enterModel.selection)
	}
}

func TestMenuModelArrowKeysExitFilteringForBrowse(t *testing.T) {
	items := []list.Item{
		menuItem{title: "repo-a"},
		menuItem{title: "repo-b"},
	}
	l := list.New(items, newMenuDelegate(), 60, 14)
	l.SetFilteringEnabled(true)

	model := menuModel{list: l}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	filteringModel := updated.(menuModel)
	if filteringModel.list.FilterState() != list.Filtering {
		t.Fatalf("expected filtering state, got %v", filteringModel.list.FilterState())
	}
	updated, _ = filteringModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r', 'e', 'p', 'o'}})
	filteringModel = updated.(menuModel)
	if filteringModel.list.FilterValue() != "repo" {
		t.Fatalf("expected filter value to be preserved, got %q", filteringModel.list.FilterValue())
	}

	updated, _ = filteringModel.Update(tea.KeyMsg{Type: tea.KeyDown})
	browsingModel := updated.(menuModel)
	if browsingModel.list.FilterState() != list.FilterApplied {
		t.Fatalf("expected filter-applied state, got %v", browsingModel.list.FilterState())
	}
	if got := browsingModel.list.Index(); got != 1 {
		t.Fatalf("expected cursor to move to second item, got index %d", got)
	}
	if browsingModel.list.FilterValue() != "repo" {
		t.Fatalf("expected filter to remain applied, got %q", browsingModel.list.FilterValue())
	}
}

func TestMenuModelWindowSizeUsesTerminalSpan(t *testing.T) {
	items := []list.Item{menuItem{title: "repo"}}
	l := list.New(items, newMenuDelegate(), 0, 0)
	model := menuModel{list: l}

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	sized := updated.(menuModel)

	if sized.list.Width() != 120 {
		t.Fatalf("expected width 120, got %d", sized.list.Width())
	}
	if sized.list.Height() != 38 {
		t.Fatalf("expected height 38, got %d", sized.list.Height())
	}
}
