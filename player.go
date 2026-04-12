package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"golang.org/x/term"
)

// Command represents a keyboard control action.
type Command int

const (
	CmdPause Command = iota
	CmdNext
	CmdPrev
	CmdQuit
	// CmdSeek is a base value; the digit (0–9) is added to it.
	// e.g. pressing 3 sends CmdSeek+3, seeking to 30% through the text.
	CmdSeek
)

// Player manages TTS playback and terminal UI.
type Player struct {
	sentences  []string
	wordCounts []int
	totalWords int
	index      int
	paused     bool
	cmd        *exec.Cmd
	cmdCh      chan Command
	tty        *os.File
	rendered   bool
}

var WPM int = 160

func NewPlayer(sentences []string, tty *os.File) *Player {
	wordCounts := make([]int, len(sentences))
	total := 0
	for i, s := range sentences {
		n := len(strings.Fields(s))
		wordCounts[i] = n
		total += n
	}
	return &Player{
		sentences:  sentences,
		wordCounts: wordCounts,
		totalWords: total,
		cmdCh:      make(chan Command, 4),
		tty:        tty,
	}
}

// Run plays sentences sequentially, handling commands from cmdCh.
func (p *Player) Run() {
	if c := exec.Command("caffeinate", "-d"); c.Start() == nil {
		defer c.Process.Kill() //nolint:errcheck
	}

	for p.index < len(p.sentences) {
		p.cmd = exec.Command("say", applyPronunciations(p.sentences[p.index])+" [[slnc 400]]")
		if err := p.cmd.Start(); err != nil {
			// If say fails, skip to next sentence.
			p.index++
			continue
		}
		p.renderUI()

		doneCh := make(chan struct{})
		go func(cmd *exec.Cmd) {
			cmd.Wait() //nolint:errcheck
			close(doneCh)
		}(p.cmd)

	outer:
		for {
			select {
			case <-doneCh:
				if !p.paused {
					p.index++
				}
				// If paused, doneCh firing means the process ended somehow;
				// treat as natural completion.
				p.paused = false
				break outer

			case c := <-p.cmdCh:
				switch c {
				case CmdQuit:
					p.cmd.Process.Kill() //nolint:errcheck
					<-doneCh
					p.clearUI()
					return

				case CmdPause:
					if !p.paused {
						p.cmd.Process.Signal(syscall.SIGSTOP) //nolint:errcheck
						p.paused = true
					} else {
						p.cmd.Process.Signal(syscall.SIGCONT) //nolint:errcheck
						p.paused = false
					}
					p.renderUI()

				case CmdNext:
					if p.paused {
						p.cmd.Process.Signal(syscall.SIGCONT) //nolint:errcheck
						p.paused = false
					}
					p.cmd.Process.Kill() //nolint:errcheck
					<-doneCh
					if p.index < len(p.sentences)-1 {
						p.index++
					}
					break outer

				case CmdPrev:
					if p.paused {
						p.cmd.Process.Signal(syscall.SIGCONT) //nolint:errcheck
						p.paused = false
					}
					p.cmd.Process.Kill() //nolint:errcheck
					<-doneCh
					if p.index > 0 {
						p.index--
					}
					break outer

				default:
					if c >= CmdSeek {
						digit := int(c - CmdSeek) // 0–9
						if p.paused {
							p.cmd.Process.Signal(syscall.SIGCONT) //nolint:errcheck
							p.paused = false
						}
						p.cmd.Process.Kill() //nolint:errcheck
						<-doneCh
						p.index = digit * len(p.sentences) / 10
						break outer
					}
				}
			}
		}
	}

	p.clearUI()
}

// keyboardLoop reads raw key input from the tty and sends Commands.
func (p *Player) keyboardLoop() {
	buf := make([]byte, 4)
	for {
		n, err := p.tty.Read(buf)
		if err != nil {
			p.cmdCh <- CmdQuit
			return
		}
		key := buf[:n]

		switch {
		case bytes.Equal(key, []byte{' '}) || bytes.Equal(key, []byte{'p'}):
			p.cmdCh <- CmdPause
		case bytes.Equal(key, []byte{'q'}) || bytes.Equal(key, []byte{3}):
			p.cmdCh <- CmdQuit
			return
		case bytes.Equal(key, []byte{27, 91, 67}): // right arrow
			p.cmdCh <- CmdNext
		case bytes.Equal(key, []byte{27, 91, 68}): // left arrow
			p.cmdCh <- CmdPrev
		case bytes.Equal(key, []byte{27, 91, 65}): // up arrow
			p.cmdCh <- CmdPrev
		case bytes.Equal(key, []byte{27, 91, 66}): // down arrow
			p.cmdCh <- CmdNext

		case len(key) == 1 && key[0] >= '0' && key[0] <= '9':
			p.cmdCh <- CmdSeek + Command(key[0]-'0')
		}
	}
}

const barWidth = 36

// contextLines controls how many sentences before and after the current one
// are shown in the UI. Total sentence rows = 2*contextLines + 1.
const contextLines = 2

// renderUI draws (or redraws) the status block to stdout.
// Total lines = 2*contextLines+1 (sentences) + 4 (blank, bar, blank, controls).
func (p *Player) renderUI() {
	if p.rendered {
		// Move cursor up to the first sentence line. The controls line has no
		// trailing newline, so the cursor sits at row R+(2*contextLines+4)-1;
		// moving up by 2*contextLines+4 returns to row R.
		fmt.Printf("\033[%dA", 2*contextLines+4)
	}

	termWidth, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || termWidth < 20 {
		termWidth = 80
	}
	// if termWidth > 80 {
	// 	termWidth = 80
	// }

	// Reserve 4 chars for the leading `  "` and trailing `"`.
	maxWidth := max(termWidth-4, 10)

	context := p.contextSentences(maxWidth)

	total := len(p.sentences)
	current := p.index + 1
	filled := min(barWidth, barWidth*current/total)
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	pct := 100 * current / total

	pauseLabel := ""
	if p.paused {
		pauseLabel = "  [PAUSED]"
	}

	wordsRemaining := 0
	for i := p.index; i < len(p.sentences); i++ {
		wordsRemaining += p.wordCounts[i]
	}
	timeLabel := ""

	secsRemaining := wordsRemaining * 60 / WPM
	if secsRemaining >= 60 {
		timeLabel = fmt.Sprintf("  ~%dm%ds left", secsRemaining/60, secsRemaining%60)
	} else {
		timeLabel = fmt.Sprintf("  ~%ds left", secsRemaining)
	}

	for i, line := range context {
		if i == contextLines {
			fmt.Printf("\r\033[K  %s\r\n", line)
		} else {
			fmt.Printf("\r\033[K\033[2m  %s\033[0m\r\n", line)
		}
	}
	fmt.Printf("\r\033[K\r\n")
	fmt.Printf("\r\033[K  [%s] %d%% (%d/%d)%s%s\r\n", bar, pct, current, total, timeLabel, pauseLabel)
	fmt.Printf("\r\033[K\r\n")
	fmt.Printf("\r\033[K  ← prev  [space] pause/resume  → next  q quit")

	p.rendered = true
}

// contextSentences returns 2*contextLines+1 sentence strings centred on the
// current index, truncated to maxWidth. Out-of-bounds entries are empty.
func (p *Player) contextSentences(maxWidth int) []string {
	lines := make([]string, 2*contextLines+1)
	for i := range lines {
		idx := p.index + i - contextLines
		if idx >= 0 && idx < len(p.sentences) {
			lines[i] = truncate(p.sentences[idx], maxWidth)
		}
	}
	return lines
}

// clearUI moves past the UI block so the shell prompt appears cleanly.
func (p *Player) clearUI() {
	if p.rendered {
		fmt.Print("\r\n")
	}
}

// truncate shortens s to at most n runes, adding "…" if cut.
func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n-1]) + "…"
}
