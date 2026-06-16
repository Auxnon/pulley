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

func TestMenuModelIgnoresEnterKeyDuringFiltering(t *testing.T) {
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
	if enterModel.action != "" || enterModel.selection != "" {
		t.Fatalf("expected no selection while filtering, got action=%q selection=%q", enterModel.action, enterModel.selection)
	}
}
