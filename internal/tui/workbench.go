package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"cairn/internal/engine"
	"cairn/internal/store"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type focusPane int

const (
	paneHorizon focusPane = iota
	paneSlate
	paneCairn
)

type inputMode int

const (
	modeNormal inputMode = iota
	modeChisel
	modeSearch
	modeSectionPrompt
)

type debounceQueryMsg struct {
	seq int
}

type sessionSavedMsg struct {
	err error
}

type editorFinishedMsg struct {
	tempPath string
	isDrop   bool
	err      error
}

// Workbench is the 3-pane Bubble Tea TUI model for Cairn.
type Workbench struct {
	store         *store.Store
	engine        *engine.Engine
	session       *store.Session
	hits          []engine.ScoredHit
	horizonCursor int
	cairnCursor   int
	activePane    focusPane
	mode          inputMode
	accordionView bool
	breadcrumbs   []string
	searchQuery   string
	statusMsg     string
	debounceSeq   int
	width         int
	height        int

	chiselInput  textarea.Model
	searchInput  textinput.Model
	sectionInput textinput.Model
}

// Run launches the interactive 3-pane Bubble Tea TUI for the specified trail.
func Run(st *store.Store, eng *engine.Engine, trailName string) error {
	sess, err := st.LoadSession(trailName)
	if err != nil {
		return fmt.Errorf("load session: %w", err)
	}
	_, _ = st.WriteTrailFile(sess)

	chisel := textarea.New()
	chisel.Placeholder = "Chisel your active problem or thesis..."
	chisel.SetValue(sess.ChiselText)
	chisel.SetHeight(3)
	chisel.ShowLineNumbers = false

	search := textinput.New()
	search.Placeholder = "Filter Horizon (/ to search, Enter to apply, Esc to clear)..."
	search.CharLimit = 160

	section := textinput.New()
	section.Placeholder = "Section header title (Enter to add ## Header, Esc to cancel)..."
	section.CharLimit = 120

	wb := &Workbench{
		store:        st,
		engine:       eng,
		session:      sess,
		activePane:   paneHorizon,
		mode:         modeNormal,
		chiselInput:  chisel,
		searchInput:  search,
		sectionInput: section,
		width:        120,
		height:       34,
		statusMsg:    fmt.Sprintf("Loaded trail %q (%d items)", sess.Name, len(sess.Items)),
	}
	wb.refreshHorizon()

	prog := tea.NewProgram(wb, tea.WithAltScreen())
	_, err = prog.Run()
	return err
}

// Init implements tea.Model.
func (w *Workbench) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (w *Workbench) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		w.width = m.Width
		w.height = m.Height
		slateW := maxInt(30, (w.width*44)/100-6)
		w.chiselInput.SetWidth(slateW)
		w.searchInput.Width = maxInt(20, (w.width*28)/100-6)
		w.sectionInput.Width = maxInt(20, (w.width*28)/100-6)
		return w, nil

	case debounceQueryMsg:
		if m.seq == w.debounceSeq {
			w.session.ChiselText = strings.TrimSpace(w.chiselInput.Value())
			w.refreshHorizon()
		}
		return w, nil

	case sessionSavedMsg:
		if m.err != nil {
			w.statusMsg = fmt.Sprintf("Save error: %v", m.err)
		}
		return w, nil

	case editorFinishedMsg:
		if m.isDrop && m.tempPath != "" {
			defer func() { _ = os.Remove(m.tempPath) }()
			if m.err == nil {
				if raw, err := os.ReadFile(m.tempPath); err == nil && strings.TrimSpace(string(raw)) != "" {
					tetherID := w.lastStackedStoneID()
					ids, err := w.store.CleaveAndWrite(string(raw), tetherID)
					if err != nil {
						w.statusMsg = fmt.Sprintf("Drop error: %v", err)
					} else if len(ids) > 0 {
						w.statusMsg = fmt.Sprintf("Dropped %d stone(s): %s", len(ids), strings.Join(ids, ", "))
					}
				}
			}
		}
		if _, err := w.store.SyncIndex(false); err != nil {
			w.statusMsg = fmt.Sprintf("Re-index error: %v", err)
		} else if !m.isDrop {
			w.statusMsg = "Re-indexed quarry and monoliths after editor exit"
		}
		if sess, err := w.store.LoadSession(w.session.Name); err == nil {
			w.session = sess
		}
		w.refreshHorizon()
		return w, nil

	case tea.KeyMsg:
		switch w.mode {
		case modeChisel:
			return w.updateChiselMode(m)
		case modeSearch:
			return w.updateSearchMode(m)
		case modeSectionPrompt:
			return w.updateSectionPromptMode(m)
		default:
			return w.updateNormalMode(m)
		}
	}

	return w, nil
}

func (w *Workbench) updateChiselMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc {
		w.chiselInput.Blur()
		w.mode = modeNormal
		w.session.ChiselText = strings.TrimSpace(w.chiselInput.Value())
		w.refreshHorizon()
		w.statusMsg = "Saved Chisel and updated Horizon"
		return w, w.persistSessionCmd()
	}

	var cmd tea.Cmd
	w.chiselInput, cmd = w.chiselInput.Update(msg)
	w.debounceSeq++
	seq := w.debounceSeq
	debounceCmd := tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg {
		return debounceQueryMsg{seq: seq}
	})
	return w, tea.Batch(cmd, debounceCmd)
}

func (w *Workbench) updateSearchMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		w.searchInput.Blur()
		w.searchInput.SetValue("")
		w.searchQuery = ""
		w.mode = modeNormal
		w.refreshHorizon()
		w.statusMsg = "Cleared search filter"
		return w, nil
	case tea.KeyEnter:
		w.searchInput.Blur()
		w.searchQuery = strings.TrimSpace(w.searchInput.Value())
		w.mode = modeNormal
		w.horizonCursor = 0
		w.refreshHorizon()
		w.statusMsg = fmt.Sprintf("Filtered Horizon by %q", w.searchQuery)
		return w, nil
	}
	var cmd tea.Cmd
	w.searchInput, cmd = w.searchInput.Update(msg)
	return w, cmd
}

func (w *Workbench) updateSectionPromptMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		w.sectionInput.Blur()
		w.sectionInput.SetValue("")
		w.mode = modeNormal
		w.statusMsg = "Cancelled section header"
		return w, nil
	case tea.KeyEnter:
		title := strings.TrimSpace(w.sectionInput.Value())
		w.sectionInput.Blur()
		w.sectionInput.SetValue("")
		w.mode = modeNormal
		if title != "" {
			w.session.Items = append(w.session.Items, store.SessionItem{
				Position:   len(w.session.Items),
				ItemType:   "header",
				HeaderText: title,
			})
			w.cairnCursor = len(w.session.Items) - 1
			w.statusMsg = fmt.Sprintf("Added section header ## %s", title)
			return w, w.persistSessionCmd()
		}
		return w, nil
	}
	var cmd tea.Cmd
	w.sectionInput, cmd = w.sectionInput.Update(msg)
	return w, cmd
}

func (w *Workbench) updateNormalMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		_ = w.store.SaveSession(w.session)
		_, _ = w.store.WriteTrailFile(w.session)
		return w, tea.Quit

	case "1":
		w.activePane = paneHorizon
		w.statusMsg = "Focused 1. HORIZON"
		return w, nil

	case "2":
		w.activePane = paneSlate
		w.statusMsg = "Focused 2. SLATE"
		return w, nil

	case "3":
		w.activePane = paneCairn
		w.statusMsg = "Focused 3. CAIRN"
		return w, nil

	case "tab":
		w.accordionView = !w.accordionView
		if w.accordionView {
			w.statusMsg = "Slate: Cairn Accordion View"
		} else {
			w.statusMsg = "Slate: Single-Stone View"
		}
		return w, nil

	case "j", "down":
		if w.activePane == paneCairn {
			if w.cairnCursor < len(w.session.Items)-1 {
				w.cairnCursor++
			}
		} else {
			if w.horizonCursor < len(w.hits)-1 {
				w.horizonCursor++
			}
		}
		return w, nil

	case "k", "up":
		if w.activePane == paneCairn {
			if w.cairnCursor > 0 {
				w.cairnCursor--
			}
		} else {
			if w.horizonCursor > 0 {
				w.horizonCursor--
			}
		}
		return w, nil

	case "l", "right":
		if note, ok := w.activeNote(); ok {
			w.breadcrumbs = append(w.breadcrumbs, note.ID)
			w.horizonCursor = 0
			w.refreshHorizon()
			w.statusMsg = fmt.Sprintf("Walked trail into %s (%d breadcrumbs)", note.ID, len(w.breadcrumbs))
		}
		return w, nil

	case "h", "left":
		if len(w.breadcrumbs) > 0 {
			popped := w.breadcrumbs[len(w.breadcrumbs)-1]
			w.breadcrumbs = w.breadcrumbs[:len(w.breadcrumbs)-1]
			w.horizonCursor = 0
			w.refreshHorizon()
			w.statusMsg = fmt.Sprintf("Stepped back from %s (%d breadcrumbs)", popped, len(w.breadcrumbs))
		}
		return w, nil

	case " ":
		if note, ok := w.activeNote(); ok {
			for _, it := range w.session.Items {
				if it.ItemType == "stone" && it.NoteID == note.ID {
					w.statusMsg = fmt.Sprintf("%s is already stacked on Cairn", note.ID)
					return w, nil
				}
			}
			w.session.Items = append(w.session.Items, store.SessionItem{
				Position:   len(w.session.Items),
				ItemType:   "stone",
				NoteID:     note.ID,
				Body:       note.Body,
				IsMonolith: note.IsMonolith,
			})
			w.cairnCursor = len(w.session.Items) - 1
			w.refreshHorizon()
			w.statusMsg = fmt.Sprintf("Stacked %s onto Cairn", note.ID)
			return w, w.persistSessionCmd()
		}
		return w, nil

	case "t":
		if note, ok := w.activeNote(); ok {
			lastID := w.lastStackedStoneID()
			if lastID == "" {
				w.statusMsg = "No stone on Cairn stack to tether to"
				return w, nil
			}
			if err := w.store.AddTrail(note.ID, lastID); err != nil {
				w.statusMsg = fmt.Sprintf("Tether error: %v", err)
				return w, nil
			}
			w.refreshHorizon()
			w.statusMsg = fmt.Sprintf("Tethered %s <-> %s in trails.tsv", note.ID, lastID)
		}
		return w, nil

	case "J":
		if len(w.session.Items) > 1 && w.cairnCursor >= 0 && w.cairnCursor < len(w.session.Items)-1 {
			idx := w.cairnCursor
			w.session.Items[idx], w.session.Items[idx+1] = w.session.Items[idx+1], w.session.Items[idx]
			w.cairnCursor++
			w.statusMsg = "Moved item down in Cairn stack"
			return w, w.persistSessionCmd()
		}
		return w, nil

	case "K":
		if len(w.session.Items) > 1 && w.cairnCursor > 0 && w.cairnCursor < len(w.session.Items) {
			idx := w.cairnCursor
			w.session.Items[idx], w.session.Items[idx-1] = w.session.Items[idx-1], w.session.Items[idx]
			w.cairnCursor--
			w.statusMsg = "Moved item up in Cairn stack"
			return w, w.persistSessionCmd()
		}
		return w, nil

	case "s":
		w.mode = modeSectionPrompt
		w.activePane = paneCairn
		w.sectionInput.SetValue("")
		w.sectionInput.Focus()
		w.statusMsg = "Enter virtual section header title..."
		return w, textinput.Blink

	case "x":
		if w.activePane == paneCairn {
			if len(w.session.Items) > 0 && w.cairnCursor >= 0 && w.cairnCursor < len(w.session.Items) {
				removed := w.session.Items[w.cairnCursor]
				w.session.Items = append(w.session.Items[:w.cairnCursor], w.session.Items[w.cairnCursor+1:]...)
				if w.cairnCursor >= len(w.session.Items) && w.cairnCursor > 0 {
					w.cairnCursor--
				}
				w.refreshHorizon()
				label := removed.NoteID
				if removed.ItemType == "header" {
					label = "## " + removed.HeaderText
				}
				w.statusMsg = fmt.Sprintf("Removed %s from Cairn stack", label)
				return w, w.persistSessionCmd()
			}
			return w, nil
		}
		if w.activePane == paneHorizon && len(w.hits) > 0 && w.horizonCursor < len(w.hits) {
			target := w.hits[w.horizonCursor].Note
			if err := w.store.Erode(target.ID); err != nil {
				w.statusMsg = fmt.Sprintf("Cannot erode: %v", err)
				return w, nil
			}
			w.refreshHorizon()
			if w.horizonCursor >= len(w.hits) && w.horizonCursor > 0 {
				w.horizonCursor--
			}
			w.statusMsg = fmt.Sprintf("Eroded %s to quarry/.trash/", target.ID)
			return w, nil
		}
		return w, nil

	case "i", "c":
		w.mode = modeChisel
		w.activePane = paneSlate
		w.chiselInput.Focus()
		w.statusMsg = "Editing Chisel (Esc to save & blur)"
		return w, textarea.Blink

	case "/":
		w.mode = modeSearch
		w.activePane = paneHorizon
		w.searchInput.Focus()
		w.statusMsg = "Search Horizon (Enter to filter, Esc to clear)"
		return w, textinput.Blink

	case "e":
		if note, ok := w.activeNote(); ok {
			path := w.store.NoteFilePath(note.ID)
			return w, w.openEditorCmd(path, false)
		}
		return w, nil

	case "n":
		tmp, err := os.CreateTemp("", "cairn-drop-*.md")
		if err != nil {
			w.statusMsg = fmt.Sprintf("Temp file error: %v", err)
			return w, nil
		}
		_ = tmp.Close()
		return w, w.openEditorCmd(tmp.Name(), true)

	case "enter":
		if w.activePane == paneCairn {
			if _, err := w.store.WriteTrailFile(w.session); err != nil {
				w.statusMsg = fmt.Sprintf("Write trail error: %v", err)
				return w, nil
			}
			trailPath := fmt.Sprintf("%s/.cairn/trail.md", w.store.RootDir)
			return w, w.openEditorCmd(trailPath, false)
		}
		return w, nil
	}

	return w, nil
}

func (w *Workbench) openEditorCmd(filePath string, isDrop bool) tea.Cmd {
	editor := strings.TrimSpace(os.Getenv("EDITOR"))
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command(editor, filePath)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorFinishedMsg{
			tempPath: filePath,
			isDrop:   isDrop,
			err:      err,
		}
	})
}

func (w *Workbench) persistSessionCmd() tea.Cmd {
	sessCopy := *w.session
	itemsCopy := make([]store.SessionItem, len(w.session.Items))
	copy(itemsCopy, w.session.Items)
	sessCopy.Items = itemsCopy

	return func() tea.Msg {
		if err := w.store.SaveSession(&sessCopy); err != nil {
			return sessionSavedMsg{err: err}
		}
		_, err := w.store.WriteTrailFile(&sessCopy)
		return sessionSavedMsg{err: err}
	}
}

func (w *Workbench) refreshHorizon() {
	var queryParts []string
	if strings.TrimSpace(w.searchQuery) != "" {
		queryParts = append(queryParts, w.searchQuery)
	}
	if strings.TrimSpace(w.session.ChiselText) != "" {
		queryParts = append(queryParts, w.session.ChiselText)
	}

	var anchors []string
	if len(w.breadcrumbs) > 0 {
		lastBreadcrumb := w.breadcrumbs[len(w.breadcrumbs)-1]
		anchors = append(anchors, lastBreadcrumb)
		if n, ok, err := w.store.GetNote(lastBreadcrumb); err == nil && ok {
			queryParts = append(queryParts, n.Body)
		}
	}
	for _, it := range w.session.Items {
		if it.ItemType == "stone" && it.NoteID != "" {
			anchors = append(anchors, it.NoteID)
		}
	}

	w.hits = w.engine.QueryHorizon(strings.Join(queryParts, " "), anchors, 16)
	if w.horizonCursor >= len(w.hits) {
		w.horizonCursor = maxInt(0, len(w.hits)-1)
	}
}

func (w *Workbench) activeNote() (store.Note, bool) {
	if w.activePane == paneCairn {
		if w.cairnCursor >= 0 && w.cairnCursor < len(w.session.Items) {
			it := w.session.Items[w.cairnCursor]
			if it.ItemType == "stone" && it.NoteID != "" {
				if n, ok, err := w.store.GetNote(it.NoteID); err == nil && ok {
					return n, true
				}
			}
		}
	}
	if w.horizonCursor >= 0 && w.horizonCursor < len(w.hits) {
		return w.hits[w.horizonCursor].Note, true
	}
	return store.Note{}, false
}

func (w *Workbench) lastStackedStoneID() string {
	for i := len(w.session.Items) - 1; i >= 0; i-- {
		if w.session.Items[i].ItemType == "stone" && w.session.Items[i].NoteID != "" {
			return w.session.Items[i].NoteID
		}
	}
	return ""
}

// View implements tea.Model.
func (w *Workbench) View() string {
	totalW := maxInt(90, w.width)
	totalH := maxInt(20, w.height)
	paneH := maxInt(14, totalH-4)

	horizonW := (totalW * 28) / 100
	cairnW := (totalW * 28) / 100
	slateW := totalW - horizonW - cairnW - 6

	focusedBorder := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("214")).
		Padding(0, 1)
	unfocusedBorder := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1)

	styleFor := func(p focusPane, wWidth int) lipgloss.Style {
		base := unfocusedBorder
		if w.activePane == p {
			base = focusedBorder
		}
		return base.Width(wWidth).Height(paneH)
	}

	horizonBox := styleFor(paneHorizon, horizonW).Render(w.renderHorizon(horizonW-2, paneH-2))
	slateBox := styleFor(paneSlate, slateW).Render(w.renderSlate(slateW-2, paneH-2))
	cairnBox := styleFor(paneCairn, cairnW).Render(w.renderCairn(cairnW-2, paneH-2))

	row := lipgloss.JoinHorizontal(lipgloss.Top, horizonBox, slateBox, cairnBox)

	helpBar := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252")).
		Background(lipgloss.Color("236")).
		Width(totalW).
		Padding(0, 1).
		Render(fmt.Sprintf("%s  |  1/2/3:Pane j/k:Nav l/h:Walk Space:Stack t:Tether J/K:Move s:Section x:Drop/Erode i:Chisel /:Search Tab:View e:Edit n:New q:Quit", w.statusMsg))

	return lipgloss.JoinVertical(lipgloss.Left, row, helpBar)
}

func (w *Workbench) renderHorizon(width, height int) string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	var b strings.Builder
	b.WriteString(titleStyle.Render("1. HORIZON"))
	if len(w.breadcrumbs) > 0 {
		b.WriteString(fmt.Sprintf(" [trail:%d]", len(w.breadcrumbs)))
	}
	b.WriteString("\n")

	if w.mode == modeSearch || w.searchQuery != "" {
		b.WriteString(w.searchInput.View())
		b.WriteString("\n")
	}

	b.WriteString(strings.Repeat("─", maxInt(10, width)))
	b.WriteString("\n")

	for idx, hit := range w.hits {
		if idx >= height-4 {
			break
		}
		cursor := "  "
		if idx == w.horizonCursor {
			cursor = "▸ "
		}
		badge := shortCategory(hit.Category)
		label := shortNoteLabel(hit.Note)
		line := fmt.Sprintf("%s[%s %.2f] %s", cursor, badge, hit.Score, label)
		if len(line) > width && width > 4 {
			line = line[:width-1] + "…"
		}
		if idx == w.horizonCursor {
			line = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229")).Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

func (w *Workbench) renderSlate(width, height int) string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	var b strings.Builder

	modeLabel := "Single-Stone"
	if w.accordionView {
		modeLabel = "Cairn Accordion"
	}
	b.WriteString(titleStyle.Render(fmt.Sprintf("2. SLATE (%s)", modeLabel)))
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("Chisel (i/c to edit, Esc to save):"))
	b.WriteString("\n")
	b.WriteString(w.chiselInput.View())
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", maxInt(10, width)))
	b.WriteString("\n")

	if w.accordionView {
		for _, item := range w.session.Items {
			if item.ItemType == "header" {
				b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("117")).Render("## " + item.HeaderText))
				b.WriteString("\n")
			} else {
				b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("• " + item.NoteID))
				b.WriteString("\n  ")
				b.WriteString(truncateText(firstLine(item.Body), maxInt(20, width-4)))
				b.WriteString("\n")
			}
		}
		return b.String()
	}

	if note, ok := w.activeNote(); ok {
		tier := "Raw Stone (lambda=0.20)"
		if note.IsMonolith {
			tier = "Carved Monolith (w=1.20, Immortal)"
		} else if note.HasCitation || note.HasTrail {
			tier = "Anchored Stone (lambda=0.03)"
		}
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("159")).Render(note.ID))
		b.WriteString("  ")
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("244")).Render("[" + tier + "]"))
		b.WriteString("\n\n")
		b.WriteString(wrapText(note.Body, maxInt(24, width-2)))
	} else {
		b.WriteString("No active stone selected.")
	}

	return b.String()
}

func (w *Workbench) renderCairn(width, height int) string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("3. CAIRN (%s)", w.session.Name)))
	b.WriteString("\n")

	if w.mode == modeSectionPrompt {
		b.WriteString(w.sectionInput.View())
		b.WriteString("\n")
	}

	b.WriteString(strings.Repeat("─", maxInt(10, width)))
	b.WriteString("\n")

	if len(w.session.Items) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("242")).Render("Stack is empty. Press Space on a stone to stack it."))
		return b.String()
	}

	for idx, item := range w.session.Items {
		if idx >= height-4 {
			break
		}
		cursor := "  "
		if idx == w.cairnCursor {
			cursor = "▸ "
		}
		var line string
		if item.ItemType == "header" {
			line = fmt.Sprintf("%s## %s", cursor, item.HeaderText)
		} else {
			preview := firstLine(item.Body)
			if preview == "" {
				preview = item.NoteID
			}
			line = fmt.Sprintf("%s[%s] %s", cursor, shortID(item.NoteID), preview)
		}
		if len(line) > width && width > 4 {
			line = line[:width-1] + "…"
		}
		if idx == w.cairnCursor {
			line = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229")).Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

func shortCategory(cat string) string {
	switch cat {
	case "Trail Neighbor":
		return "TRAIL"
	case "Direct Match":
		return "MATCH"
	case "Stepping Stone":
		return "BRIDGE"
	case "Monolith":
		return "MONO"
	default:
		return "QUARRY"
	}
}

func shortID(id string) string {
	if strings.HasPrefix(id, "monolith:") {
		slug := strings.TrimPrefix(id, "monolith:")
		if len(slug) > 12 {
			return "m:" + slug[:12]
		}
		return "m:" + slug
	}
	if len(id) > 10 {
		return id[:10]
	}
	return id
}

func shortNoteLabel(n store.Note) string {
	fl := firstLine(n.Body)
	if fl != "" {
		return fl
	}
	return n.ID
}

func firstLine(s string) string {
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		trimmed := strings.TrimSpace(strings.TrimPrefix(line, "#"))
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func truncateText(s string, maxLen int) string {
	if len(s) <= maxLen || maxLen <= 3 {
		return s
	}
	return s[:maxLen-1] + "…"
}

func wrapText(s string, width int) string {
	if width <= 10 {
		return s
	}
	var out strings.Builder
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out.WriteByte('\n')
			continue
		}
		lineLen := 0
		for i, w := range words {
			if i > 0 && lineLen+1+len(w) > width {
				out.WriteByte('\n')
				lineLen = 0
			} else if i > 0 {
				out.WriteByte(' ')
				lineLen++
			}
			out.WriteString(w)
			lineLen += len(w)
		}
		out.WriteByte('\n')
	}
	return out.String()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
