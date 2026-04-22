package screen

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/klaassen-consulting/jc/internal/recipe"
	"github.com/klaassen-consulting/jc/internal/tui"
	"github.com/klaassen-consulting/jc/internal/tui/style"
)

// RecipeDispatcher is the command dispatcher used to execute recipe steps.
// Assembly code in cmd/tui.go sets this before launching the TUI, to avoid
// a cmd ↔ tui import cycle. If nil when a recipe is run, the screen
// surfaces a configuration error instead of crashing.
var RecipeDispatcher recipe.CommandDispatcher

// Progress line format emitted by recipe.Execute() (see internal/recipe/recipe.go).
// Examples:
//   [1/3] create-user... done
//   [2/3] add-to-group... failed
//   [3/3] verify-user... skipped
var progressLineRe = regexp.MustCompile(`^\[(\d+)/(\d+)\]\s+(.+?)\.\.\.\s+(done|failed|skipped)$`)

// recipeStepState tracks the runtime status of a single step on the run screen.
type recipeStepState struct {
	name   string
	status string // "pending" | "running" | "done" | "failed" | "skipped"
}

// recipeStartMsg is emitted just before dispatcher invocation for a step, so
// the UI can flip pending → running before the step completes.
// The engine doesn't emit this directly; we synthesize it by tracking which
// step should be running next based on prior completions.
type recipeStartMsg struct{ stepIdx int }

// recipeDoneMsg is emitted when Execute returns.
type recipeDoneMsg struct {
	result *recipe.ExecutionResult
	err    error
}

// recipeLineMsg is emitted by the pipe-draining Cmd with one raw progress line.
type recipeLineMsg struct{ line string }

// RecipeRunScreen shows live step-by-step execution of a recipe.
type RecipeRunScreen struct {
	recipe   *recipe.Recipe
	params   map[string]string
	planMode bool

	steps    []recipeStepState
	spinner  spinner.Model
	done     bool
	result   *recipe.ExecutionResult
	err      string
	planText string

	// Plumbing for async execution.
	pipeR      *io.PipeReader
	pipeW      *io.PipeWriter
	pipeReader *bufio.Reader // created once when the pipe is; prevents buffered-data loss across reads

	width  int
	height int

	// scrollOffset controls the first visible line of the scrollable body.
	// Updated on j/k/up/down/page keys; clamped in clampScroll.
	scrollOffset int
}

// NewRecipeRunScreen creates the run screen. When planMode is true, the screen
// renders the plan preview and does not execute.
func NewRecipeRunScreen(r *recipe.Recipe, params map[string]string, planMode bool) *RecipeRunScreen {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = style.Spinner

	s := &RecipeRunScreen{
		recipe:   r,
		params:   params,
		planMode: planMode,
		spinner:  sp,
	}
	s.steps = make([]recipeStepState, len(r.Steps))
	for i, st := range r.Steps {
		s.steps[i] = recipeStepState{name: st.Name, status: "pending"}
	}
	return s
}

func (s *RecipeRunScreen) Title() string {
	if s.planMode {
		return "Plan: " + s.recipe.Name
	}
	return "Run: " + s.recipe.Name
}

func (s *RecipeRunScreen) Init() tea.Cmd {
	if s.planMode {
		plans, err := s.recipe.Plan(s.params)
		if err != nil {
			s.err = err.Error()
			s.done = true
			return nil
		}
		var b strings.Builder
		recipe.RenderPlanHuman(&b, s.recipe.Name, plans)
		s.planText = b.String()
		s.done = true
		return nil
	}

	if RecipeDispatcher == nil {
		s.err = "recipe dispatcher not configured (internal error)"
		s.done = true
		return nil
	}

	// Set up async execution. The goroutine writes progress to pipeW;
	// tea.Cmd drains pipeR one line at a time and emits recipeLineMsg.
	// Note: recipe.Execute takes no context today, so esc can leave the
	// screen but the in-flight step will finish before the goroutine exits.
	// Wiring context.Context into Execute is tracked as a follow-up.
	s.pipeR, s.pipeW = io.Pipe()
	// One bufio.Reader for the lifetime of the pipe so buffered data
	// (anything read past the \n) isn't discarded between Cmd invocations.
	s.pipeReader = bufio.NewReader(s.pipeR)

	go func() {
		result, err := s.recipe.Execute(RecipeDispatcher, s.params, s.pipeW)
		_ = s.pipeW.Close()
		teaProgramSend(recipeDoneMsg{result: result, err: err})
	}()

	// Mark step 0 as running immediately.
	return tea.Batch(
		s.spinner.Tick,
		func() tea.Msg { return recipeStartMsg{stepIdx: 0} },
		s.readNextLine(),
	)
}

// readNextLine returns a Cmd that reads one line from the progress pipe.
// It emits recipeLineMsg on success; on EOF or error, it emits nothing
// (the completion signal comes from the goroutine's recipeDoneMsg).
func (s *RecipeRunScreen) readNextLine() tea.Cmd {
	reader := s.pipeReader
	return func() tea.Msg {
		if reader == nil {
			return nil
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil
		}
		return recipeLineMsg{line: strings.TrimRight(line, "\n")}
	}
}

func (s *RecipeRunScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
		s.height = msg.Height
		return s, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			// Leaving the screen: the goroutine continues until the current
			// step finishes, then exits cleanly when it writes to the closed
			// pipe. No state is leaked.
			if !s.done && s.pipeR != nil {
				_ = s.pipeR.Close()
			}
			return s, func() tea.Msg { return tui.PopScreenMsg{} }
		case "enter":
			if s.done {
				return s, func() tea.Msg { return tui.PopScreenMsg{} }
			}
		case "j", "down":
			s.scrollOffset++
			return s, nil
		case "k", "up":
			s.scrollOffset--
			if s.scrollOffset < 0 {
				s.scrollOffset = 0
			}
			return s, nil
		case "g":
			s.scrollOffset = 0
			return s, nil
		case "G":
			s.scrollOffset = 1 << 30 // clamped in View
			return s, nil
		case "pgdown", " ":
			s.scrollOffset += s.viewportHeight()
			return s, nil
		case "pgup":
			s.scrollOffset -= s.viewportHeight()
			if s.scrollOffset < 0 {
				s.scrollOffset = 0
			}
			return s, nil
		}

	case recipeLineMsg:
		s.applyProgressLine(msg.line)
		// Schedule another read.
		return s, s.readNextLine()

	case recipeStartMsg:
		if msg.stepIdx >= 0 && msg.stepIdx < len(s.steps) && s.steps[msg.stepIdx].status == "pending" {
			s.steps[msg.stepIdx].status = "running"
		}
		return s, nil

	case recipeDoneMsg:
		s.done = true
		s.result = msg.result
		if msg.err != nil {
			s.err = msg.err.Error()
		}
		if s.pipeR != nil {
			_ = s.pipeR.Close()
		}
		return s, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		s.spinner, cmd = s.spinner.Update(msg)
		return s, cmd
	}

	return s, nil
}

// applyProgressLine parses one line of engine progress output and updates
// the corresponding step's status. It also flips the next step to "running"
// so the UI shows forward motion.
func (s *RecipeRunScreen) applyProgressLine(line string) {
	m := progressLineRe.FindStringSubmatch(line)
	if m == nil {
		return
	}
	idx, err := strconv.Atoi(m[1])
	if err != nil || idx < 1 || idx > len(s.steps) {
		return
	}
	stepIdx := idx - 1 // 1-based → 0-based
	status := m[4]

	s.steps[stepIdx].status = status

	// Advance to the next step if there is one and we didn't fail.
	if status != "failed" && stepIdx+1 < len(s.steps) && s.steps[stepIdx+1].status == "pending" {
		s.steps[stepIdx+1].status = "running"
	}
}

// teaProgramSend is set by the App when the program is running so the
// goroutine can push recipeDoneMsg back onto the update loop. Overridable
// for tests; in the real app it wraps (*tea.Program).Send.
var teaProgramSend = func(msg tea.Msg) {
	// Default is a no-op; wiring is done below via RegisterTeaProgram.
}

// RegisterTeaProgram sets the global program-send hook. Called from
// cmd/tui.go once the program is constructed. Kept as a free function to
// avoid weaving a *tea.Program pointer through every screen constructor.
func RegisterTeaProgram(p interface{ Send(tea.Msg) }) {
	teaProgramSend = p.Send
}

// viewportHeight returns how many lines are available for the scrollable
// body (everything except the fixed title and footer rows).
func (s *RecipeRunScreen) viewportHeight() int {
	// Title + blank + footer = 3 lines; plus 1 for scroll indicator when needed.
	reserved := 4
	h := s.height - reserved
	if h < 5 {
		h = 5
	}
	return h
}

// buildBodyLines assembles the scrollable body: per-step status, step output,
// final error/summary message. Returns a slice of already-styled lines.
func (s *RecipeRunScreen) buildBodyLines() []string {
	if s.planMode {
		out := []string{}
		if s.err != "" {
			out = append(out, style.Error.Render("  "+s.err))
		} else {
			for _, line := range strings.Split(strings.TrimRight(s.planText, "\n"), "\n") {
				out = append(out, line)
			}
		}
		return out
	}

	// Build a quick lookup from step name → captured output, available only
	// after the recipe finishes (recipeDoneMsg populates s.result).
	outputs := map[string]string{}
	if s.result != nil {
		for _, sr := range s.result.Steps {
			outputs[sr.Name] = sr.Output
		}
	}

	var lines []string
	for _, st := range s.steps {
		icon := "○"
		lineStyle := style.DimRow
		switch st.status {
		case "running":
			icon = s.spinner.View()
			lineStyle = style.ResourceName
		case "done":
			icon = "✓"
			lineStyle = style.ResourceName
		case "skipped":
			icon = "⊘"
			lineStyle = style.DimRow
		case "failed":
			icon = "✗"
			lineStyle = style.Error
		}
		lines = append(lines, lineStyle.Render(fmt.Sprintf("  %s %s", icon, st.name)))

		// Show captured output below the step line once available. Keep the
		// "skipped" case output-free (engine doesn't produce output for
		// skipped steps, and showing empty whitespace is just noise).
		if out, ok := outputs[st.name]; ok && st.status != "skipped" {
			for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
				if line == "" {
					lines = append(lines, "")
					continue
				}
				lines = append(lines, "      "+line)
			}
			if st.status == "done" || st.status == "failed" {
				lines = append(lines, "")
			}
		}
	}

	if s.err != "" {
		lines = append(lines, "")
		lines = append(lines, style.Error.Render("  "+s.err))
	}

	if s.done && s.result != nil && s.result.Message != "" {
		lines = append(lines, "")
		lines = append(lines, style.Category.Render("  "+s.result.Message))
	}

	return lines
}

// clampScroll constrains scrollOffset to [0, max(0, totalLines - viewport)].
func (s *RecipeRunScreen) clampScroll(totalLines int) {
	max := totalLines - s.viewportHeight()
	if max < 0 {
		max = 0
	}
	if s.scrollOffset > max {
		s.scrollOffset = max
	}
	if s.scrollOffset < 0 {
		s.scrollOffset = 0
	}
}

func (s *RecipeRunScreen) View() string {
	var sb strings.Builder

	// Title.
	if s.planMode {
		sb.WriteString(style.Title.Render("Plan: " + s.recipe.Name))
	} else {
		sb.WriteString(style.Title.Render("Run: " + s.recipe.Name))
	}
	sb.WriteString("\n\n")

	// Body (scrollable).
	body := s.buildBodyLines()
	s.clampScroll(len(body))

	vh := s.viewportHeight()
	start := s.scrollOffset
	end := start + vh
	if end > len(body) {
		end = len(body)
	}
	for i := start; i < end; i++ {
		sb.WriteString(body[i])
		sb.WriteString("\n")
	}
	// Fill short bodies so footer doesn't jump.
	for i := end - start; i < vh; i++ {
		sb.WriteString("\n")
	}

	// Scroll indicator + keys footer.
	scrollHint := ""
	if len(body) > vh {
		scrollHint = fmt.Sprintf("  [%d-%d of %d]  j/k: scroll  g/G: top/bottom", start+1, end, len(body))
	}

	var keys string
	if s.done {
		keys = "enter: back  esc: back"
	} else {
		keys = "esc: back (running in background)"
	}
	if scrollHint != "" {
		sb.WriteString(style.DimRow.Render(scrollHint))
		sb.WriteString("\n")
	}
	sb.WriteString(style.DimRow.Render("  " + keys))

	return sb.String()
}
