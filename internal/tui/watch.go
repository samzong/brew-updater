package tui

import (
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type Item struct {
	Name string
	Type string
}

type Selection struct {
	Name        string
	Type        string
	Policy      string
	IntervalMin int
}

type mode int

const (
	modeList mode = iota
	modeSearch
	modeInterval
)

type model struct {
	items           []Item
	selected        map[string]bool
	policy          map[string]string
	intervalMin     map[string]int
	cursor          int
	offset          int
	filter          string
	mode            mode
	input           textinput.Model
	status          string
	defaultPolicy   string
	defaultInterval int
	cancelled       bool
	width           int
	height          int
}

func RunWatch(items []Item, defaultPolicy string, defaultInterval int, preset map[string]Selection) ([]Selection, bool, error) {
	m := newModel(items, defaultPolicy, defaultInterval, preset)
	p := tea.NewProgram(m)
	res, err := p.Run()
	if err != nil {
		return nil, false, err
	}
	final := res.(model)
	if final.cancelled {
		return nil, true, nil
	}
	return final.selectedItems(), false, nil
}

func newModel(items []Item, defaultPolicy string, defaultInterval int, preset map[string]Selection) model {
	ti := textinput.New()
	ti.CharLimit = 64
	m := model{
		items:           items,
		selected:        make(map[string]bool),
		policy:          make(map[string]string),
		intervalMin:     make(map[string]int),
		cursor:          0,
		offset:          0,
		filter:          "",
		mode:            modeList,
		input:           ti,
		status:          "",
		defaultPolicy:   defaultPolicy,
		defaultInterval: defaultInterval,
		cancelled:       false,
	}
	for key, sel := range preset {
		m.selected[key] = true
		if sel.Policy != "" {
			m.policy[key] = sel.Policy
		}
		if sel.IntervalMin > 0 {
			m.intervalMin[key] = sel.IntervalMin
		}
	}
	return m
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeSearch:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "enter":
				m.filter = m.input.Value()
				m.cursor = 0
				m.offset = 0
				m.mode = modeList
				m.input.SetValue("")
				return m, nil
			case "esc":
				m.filter = ""
				m.cursor = 0
				m.offset = 0
				m.mode = modeList
				m.input.SetValue("")
				return m, nil
			}
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	case modeInterval:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "enter":
				val := strings.TrimSpace(m.input.Value())
				m.input.SetValue("")
				m.mode = modeList
				if val == "" {
					m.status = "interval empty"
					return m, nil
				}
				n, err := strconv.Atoi(val)
				if err != nil || n < 1 || n > 1440 {
					m.status = "interval must be 1-1440"
					return m, nil
				}
				for name := range m.selected {
					if m.selected[name] {
						m.intervalMin[name] = n
					}
				}
				m.status = fmt.Sprintf("interval set to %d", n)
				return m, nil
			case "esc":
				m.mode = modeList
				m.input.SetValue("")
				return m, nil
			}
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	default:
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "ctrl+c", "q":
				m.cancelled = true
				return m, tea.Quit
			case "up", "k", "ctrl+p":
				if m.cursor > 0 {
					m.cursor--
				}
				m.ensureVisible()
			case "down", "j", "ctrl+n":
				if m.cursor < len(m.filtered())-1 {
					m.cursor++
				}
				m.ensureVisible()
			case " ":
				m.toggleCurrent()
			case "a":
				m.toggleAll()
			case "x":
				m.invertSelection()
			case "/":
				m.mode = modeSearch
				m.input.Placeholder = "search"
				m.input.Focus()
				return m, nil
			case "i":
				m.mode = modeInterval
				m.input.Placeholder = "interval (1-1440)"
				m.input.Focus()
				return m, nil
			case "p":
				m.togglePolicy()
			case "enter":
				return m, tea.Quit
			}
		case tea.WindowSizeMsg:
			m.width = msg.Width
			m.height = msg.Height
			m.ensureVisible()
		}
	}
	return m, nil
}

func (m model) View() string {
	if len(m.items) == 0 {
		return "No installable packages found."
	}
	b := strings.Builder{}
	b.WriteString("brew-updater watch\n")
	b.WriteString(fmt.Sprintf("filter: %s | selected: %d\n", m.filter, m.selectedCount()))
	b.WriteString("\n")

	filtered := m.filtered()
	if len(filtered) == 0 {
		b.WriteString("No matches.\n")
	} else {
		start, end := m.visibleRange(len(filtered))
		tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		for i := start; i < end; i++ {
			idx := filtered[i]
			item := m.items[idx]
			key := itemKey(item)
			cursor := " "
			if i == m.cursor {
				cursor = ">"
			}
			checked := "[ ]"
			if m.selected[key] {
				checked = "[x]"
			}
			policy := m.policyValue(key)
			interval := m.intervalValue(key)
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\tpolicy=%s\tinterval=%dm\n", cursor, checked, item.Name, item.Type, policy, interval)
		}
		_ = tw.Flush()
	}

	b.WriteString("\nKeys: up/down=j/k/ctrl+n/ctrl+p | space=toggle | a=all/unall | x=invert | /=search | i=interval | p=policy | enter=save | q=quit\n")
	if m.mode == modeSearch {
		b.WriteString("Search: " + m.input.View() + "\n")
	}
	if m.mode == modeInterval {
		b.WriteString("Interval: " + m.input.View() + "\n")
	}
	if m.status != "" {
		b.WriteString("\n" + m.status + "\n")
	}
	return b.String()
}

func (m *model) filtered() []int {
	if m.filter == "" {
		idx := make([]int, 0, len(m.items))
		for i := range m.items {
			idx = append(idx, i)
		}
		return idx
	}
	idx := []int{}
	q := strings.ToLower(m.filter)
	for i, item := range m.items {
		if strings.Contains(strings.ToLower(item.Name), q) {
			idx = append(idx, i)
		}
	}
	if m.cursor >= len(idx) {
		m.cursor = 0
		m.offset = 0
	}
	return idx
}

func (m *model) toggleCurrent() {
	filtered := m.filtered()
	if len(filtered) == 0 || m.cursor >= len(filtered) {
		return
	}
	item := m.items[filtered[m.cursor]]
	key := itemKey(item)
	m.selected[key] = !m.selected[key]
	if m.selected[key] {
		if _, ok := m.policy[key]; !ok {
			m.policy[key] = m.defaultPolicy
		}
		if _, ok := m.intervalMin[key]; !ok {
			m.intervalMin[key] = m.defaultInterval
		}
	}
}

func (m *model) selectAll() {
	for _, idx := range m.filtered() {
		item := m.items[idx]
		key := itemKey(item)
		m.selected[key] = true
		if _, ok := m.policy[key]; !ok {
			m.policy[key] = m.defaultPolicy
		}
		if _, ok := m.intervalMin[key]; !ok {
			m.intervalMin[key] = m.defaultInterval
		}
	}
}

func (m *model) toggleAll() {
	if m.allSelected() {
		for _, idx := range m.filtered() {
			item := m.items[idx]
			m.selected[itemKey(item)] = false
		}
		return
	}
	m.selectAll()
}

func (m *model) allSelected() bool {
	filtered := m.filtered()
	if len(filtered) == 0 {
		return false
	}
	for _, idx := range filtered {
		item := m.items[idx]
		if !m.selected[itemKey(item)] {
			return false
		}
	}
	return true
}

func (m *model) invertSelection() {
	for _, idx := range m.filtered() {
		item := m.items[idx]
		key := itemKey(item)
		m.selected[key] = !m.selected[key]
		if m.selected[key] {
			if _, ok := m.policy[key]; !ok {
				m.policy[key] = m.defaultPolicy
			}
			if _, ok := m.intervalMin[key]; !ok {
				m.intervalMin[key] = m.defaultInterval
			}
		}
	}
}

func (m *model) togglePolicy() {
	for name := range m.selected {
		if !m.selected[name] {
			continue
		}
		cur := m.policyValue(name)
		if cur == "auto" {
			m.policy[name] = "notify"
		} else {
			m.policy[name] = "auto"
		}
	}
}

func (m model) policyValue(key string) string {
	if v, ok := m.policy[key]; ok && v != "" {
		return v
	}
	return m.defaultPolicy
}

func (m model) intervalValue(key string) int {
	if v, ok := m.intervalMin[key]; ok && v > 0 {
		return v
	}
	return m.defaultInterval
}

func (m model) selectedItems() []Selection {
	items := []Selection{}
	for _, item := range m.items {
		key := itemKey(item)
		if !m.selected[key] {
			continue
		}
		items = append(items, Selection{
			Name:        item.Name,
			Type:        item.Type,
			Policy:      m.policyValue(key),
			IntervalMin: m.intervalValue(key),
		})
	}
	return items
}

func (m model) selectedCount() int {
	count := 0
	for _, v := range m.selected {
		if v {
			count++
		}
	}
	return count
}

func (m *model) ensureVisible() {
	if m.height <= 0 {
		return
	}
	total := len(m.filtered())
	if total == 0 {
		m.cursor = 0
		m.offset = 0
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= total {
		m.cursor = total - 1
	}
	height := m.listHeight()
	if height <= 0 {
		m.offset = 0
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+height {
		m.offset = m.cursor - height + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
	maxOffset := total - height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.offset > maxOffset {
		m.offset = maxOffset
	}
}

func (m model) listHeight() int {
	if m.height <= 0 {
		return 0
	}
	lines := 0
	lines += 1 // title
	lines += 1 // filter
	lines += 1 // blank
	lines += 1 // blank after list
	lines += 1 // keys
	if m.mode == modeSearch {
		lines += 1
	}
	if m.mode == modeInterval {
		lines += 1
	}
	if m.status != "" {
		lines += 2 // blank + status
	}
	height := m.height - lines
	if height < 1 {
		return 1
	}
	return height
}

func (m model) visibleRange(total int) (int, int) {
	height := m.listHeight()
	if height <= 0 || total <= height {
		return 0, total
	}
	start := m.offset
	if start < 0 {
		start = 0
	}
	if start > total-height {
		start = total - height
	}
	end := start + height
	if end > total {
		end = total
	}
	return start, end
}

func itemKey(item Item) string {
	return selectionKey(item.Name, item.Type)
}

func selectionKey(name string, typ string) string {
	if typ == "" {
		return name
	}
	return typ + ":" + name
}
