package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	Fetch    key.Binding
	FetchAll key.Binding
	Pull     key.Binding
	Edit     key.Binding
	Term     key.Binding
	Run      key.Binding
	Refresh  key.Binding
	Filter   key.Binding
	Sort     key.Binding
	Detail   key.Binding
	Quit     key.Binding
	Help     key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Fetch:    key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "fetch")),
		FetchAll: key.NewBinding(key.WithKeys("F"), key.WithHelp("F", "fetch all")),
		Pull:     key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "pull")),
		Edit:     key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		Term:     key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "term")),
		Run:      key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "cmd")),
		Refresh:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Filter:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Sort:     key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort")),
		Detail:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "detail")),
		Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Help:     key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Fetch, k.FetchAll, k.Pull, k.Edit, k.Term, k.Run, k.Filter, k.Help, k.Quit}
}
