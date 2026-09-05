package tui

import (
	"io"
	"slices"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// outcome is how a screen ended: the answer taken, Esc, or Ctrl-C.
type outcome int

const (
	next outcome = iota
	back
	quit
)

// screen is one question on the terminal: a huh form with a single field,
// run as its own bubbletea program. Esc ends it with back, and the frame
// left on the terminal once it's answered is one line, the answer, so the
// terminal reads as the list of decisions made.
type screen struct {
	form    *huh.Form
	summary func() string
	help    help.Model
	outcome outcome
	done    bool
}

var escape = key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back"))

// newScreen wraps one field.
func newScreen(field huh.Field, summary func() string) *screen {
	return newPages(summary, huh.NewGroup(field))
}

// newPages wraps a form of one group per page, shown one after the other:
// a list, then the one thing its backend needs. A Select's filter is off,
// since its lists are short and the filter's own Esc would fight the
// screen's; a MultiSelect keeps its filter, and the screen hands Esc to
// the field while the filter is open.
func newPages(summary func() string, groups ...*huh.Group) *screen {
	theme := huh.ThemeCharm()
	helper := help.New()
	helper.Styles = theme.Help
	keys := huh.NewDefaultKeyMap()
	keys.Select.Filter.SetEnabled(false)
	keys.Select.SetFilter.SetEnabled(false)
	keys.Select.ClearFilter.SetEnabled(false)
	form := huh.NewForm(groups...).WithTheme(theme).WithKeyMap(keys).WithShowHelp(false)
	return &screen{form: form, summary: summary, help: helper}
}

func (s *screen) Init() tea.Cmd {
	return s.form.Init()
}

func (s *screen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if s.done {
		return s, nil
	}
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		size = sized(size)
		msg = size
		// One line stays free for the help below the form.
		s.form = s.form.WithHeight(size.Height - 1)
	}
	if pressed, ok := msg.(tea.KeyMsg); ok && pressed.Type == tea.KeyEsc && !s.fieldTakesEsc() {
		s.done, s.outcome = true, back
		return s, tea.Quit
	}
	model, cmd := s.form.Update(msg)
	s.form = model.(*huh.Form)
	// The form's own Run would quit the program here; this screen runs the
	// program itself, so it quits itself.
	switch s.form.State {
	case huh.StateCompleted:
		s.done, s.outcome = true, next
		return s, tea.Quit
	case huh.StateAborted:
		s.done, s.outcome = true, quit
		return s, tea.Quit
	}
	return s, cmd
}

// fieldTakesEsc reports whether the focused field has a use for Esc right
// now, closing or clearing its filter, so the screen's back waits for the
// next one.
func (s *screen) fieldTakesEsc() bool {
	for _, binding := range s.form.GetFocusedField().KeyBinds() {
		if binding.Enabled() && slices.Contains(binding.Keys(), "esc") {
			return true
		}
	}
	return false
}

// View is the form while the question is open, then the one line that
// stays behind. The line ends with a newline so the terminal keeps it:
// bubbletea erases the line the cursor ends on. Esc and Ctrl-C leave
// nothing, since the question is either shown again or the run is over,
// and so does a screen with no line of its own, one round of a backend
// whose line comes when the backend ends.
func (s *screen) View() string {
	if !s.done {
		return s.form.View() + "\n" + s.help.ShortHelpView(s.bindings())
	}
	if s.outcome != next {
		return ""
	}
	line := s.summary()
	if line == "" {
		return ""
	}
	return line + "\n"
}

// bindings is the focused field's own key help plus Esc, which the screen
// handles before the field sees it.
func (s *screen) bindings() []key.Binding {
	var enabled []key.Binding
	for _, binding := range s.form.GetFocusedField().KeyBinds() {
		if binding.Enabled() {
			enabled = append(enabled, binding)
		}
	}
	return append(enabled, escape)
}

// fallbackSize stands in for a terminal that reports no size, a pty
// nothing sized: the classic 80 by 24. Without it the list's filter
// input gets a negative width and bubbles panics.
var fallbackSize = tea.WindowSizeMsg{Width: 80, Height: 24}

func sized(size tea.WindowSizeMsg) tea.WindowSizeMsg {
	if size.Width <= 0 || size.Height <= 0 {
		return fallbackSize
	}
	return size
}

// runner runs one screen to its end and hands it back with its outcome.
type runner func(*screen) error

// terminal is the runner on a real terminal: the wait line comes off,
// then the screen takes the terminal.
func terminal(in io.Reader, out io.Writer, waiting *wait) runner {
	return func(s *screen) error {
		waiting.clear()
		_, err := tea.NewProgram(s, tea.WithInput(in), tea.WithOutput(out)).Run()
		return err
	}
}
