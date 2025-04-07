package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// styles
var (
	docStyle      = lipgloss.NewStyle().Margin(1, 2)
	titleStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	itemStyle     = lipgloss.NewStyle().PaddingLeft(2)
	selectedStyle = lipgloss.NewStyle().PaddingLeft(0).Foreground(lipgloss.Color("205")).Bold(true)
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

// list.Item implemtation for branches
type branchItem string

func (i branchItem) FilterValue() string {
	return string(i)
}
func (i branchItem) Title() string {
	return string(i)
}
func (i branchItem) Description() string {
	return ""
}

// messages
type branchesLoadedMsg struct {
	items         []list.Item
	currentBranch string
}
type branchCheckoutResultMsg struct {
	success bool
	branch  string
	err     error
}
type errMsg struct{ err error }

func (e errMsg) Error() string {
	return e.err.Error()
}

// modek
type model struct {
	list          list.Model
	spinner       spinner.Model
	loading       bool
	checkingOut   bool
	currentBranch string
	err           error
	quitting      bool
}

func initialModel() model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))

	l := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Git branches"
	l.SetSpinner(s.Spinner)
	l.SetShowHelp(false)

	delegate := list.NewDefaultDelegate()
	delegate.Styles.NormalTitle = itemStyle
	delegate.Styles.SelectedTitle = selectedStyle
	delegate.Styles.SelectedDesc = selectedStyle // Keep description style consistent
	l.SetDelegate(delegate)

	return model{
		spinner: s,
		list:    l,
		loading: true, // Start in loading state to fetch initial branches
	}
}

func fetchBranches() tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("git", "branch", "-a")
		var outb, errb bytes.Buffer
		cmd.Stdout = &outb
		cmd.Stdout = &errb
		err := cmd.Run()

		if err != nil {
			errMsgContent := errb.String()
			if errMsgContent == "" {
				errMsgContent = err.Error()
			}
			return errMsg{fmt.Errorf("failed to list branches: %s", errMsgContent)}
		}
		lines := strings.Split(strings.TrimSpace(outb.String()), "\n")
		items := make([]list.Item, 0, len(lines))
		current := ""
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			branchName := trimmed
			if strings.HasPrefix(trimmed, "* ") {
				branchName = strings.TrimPrefix(trimmed, "* ")
				current = branchName
			}
			items = append(items, branchItem(branchName))
		}
		return branchesLoadedMsg{items: items, currentBranch: current}
	}
}
func checkoutBranch(branchName string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("git", "checkout", branchName)
		var errb bytes.Buffer
		cmd.Stderr = &errb // Capture stderr for error messages
		err := cmd.Run()

		if err != nil {
			errMsgContent := errb.String()
			if errMsgContent == "" {
				errMsgContent = err.Error()
			}
			// Return error details in the message
			return branchCheckoutResultMsg{success: false, branch: branchName, err: fmt.Errorf(errMsgContent)}
		}
		// Return success
		return branchCheckoutResultMsg{success: true, branch: branchName}
	}
}

// bubble tea
func (m model) Init() tea.Cmd {
	// Start the spinner and fetch initial branches
	return tea.Batch(m.spinner.Tick, fetchBranches())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Don't allow input if loading or checking out
		if m.loading || m.checkingOut {
			return m, nil
		}
		switch keypress := msg.String(); keypress {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit
		case "enter":
			selectedItem := m.list.SelectedItem()
			if selectedItem != nil {
				branchName := string(selectedItem.(branchItem))
				if branchName != m.currentBranch {
					m.checkingOut = true
					m.err = nil
					return m, tea.Batch(m.spinner.Tick, checkoutBranch(branchName))
				}
			}
		}
	case tea.WindowSizeMsg:
		h, v := docStyle.GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v)
	case branchesLoadedMsg:
		m.loading = false
		m.list.SetItems(msg.items)
		m.currentBranch = msg.currentBranch
		for i, item := range msg.items {
			if string(item.(branchItem)) == m.currentBranch {
				m.list.Select(i)
				break
			}
		}
		m.list.StopSpinner()
	case branchCheckoutResultMsg:
		m.checkingOut = false
		if !msg.success {
			m.err = msg.err
		} else {
			m.currentBranch = msg.branch
			m.err = nil
			m.loading = true
			m.list.ResetSelected()
			cmds = append(cmds, fetchBranches(), m.spinner.Tick)
		}
	case errMsg:
		m.err = msg.err
		m.loading = false
		m.checkingOut = false
		m.list.StopSpinner()
		return m, nil
	case spinner.TickMsg:
		if m.loading || m.checkingOut {
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}
	}
	if !m.loading && !m.checkingOut {
		m.list, cmd = m.list.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if m.quitting {
		return docStyle.Render("Checking out... Done. Bye!\n")
	}

	var viewContent strings.Builder
	title := titleStyle.Render("Interactive Git Branch Viewer")
	status := ""
	if m.loading {
		status = m.spinner.View() + " Loading branches..."
	} else if m.checkingOut {
		status = m.spinner.View() + " Checking out branch..."
	} else if m.err != nil {
		status = errorStyle.Render("Error: " + m.err.Error())
	} else {
		status = "Current branch: " + titleStyle.Render(m.currentBranch)
	}
	viewContent.WriteString(title + "\n" + status + "\n\n")
	if !m.loading && !m.checkingOut && m.err == nil || (m.err != nil && len(m.list.Items()) > 0) { // Show list even if checkout failed
		viewContent.WriteString(m.list.View())
	} else if !m.loading && !m.checkingOut && m.err != nil {
		// If there was an error AND the list is empty (e.g., initial fetch failed)
		// The error is already displayed in the status line. Maybe add extra info here if needed.
		viewContent.WriteString("\n" + errorStyle.Render("Could not load branch list."))
	}
	help := helpStyle.Render("\n↑/↓: Navigate | Enter: Checkout | q/ctrl+c: Quit")
	viewContent.WriteString(help)

	// Apply overall document styling
	return docStyle.Render(viewContent.String())
}

func main() {
	if _, err := exec.LookPath("git"); err != nil {
		fmt.Println("Error: git command not found. Please install Git.")
		os.Exit(1)
	}
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	if err := cmd.Run(); err != nil {
		fmt.Println("Error: Not inside a Git repository.")
		os.Exit(1)
	}

	m := initialModel()
	p := tea.NewProgram(m, tea.WithAltScreen()) // Use AltScreen for cleaner exit

	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}
