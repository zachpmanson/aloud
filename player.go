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
	sentences []string
	index     int
	paused    bool
	cmd       *exec.Cmd
	cmdCh     chan Command
	tty       *os.File
	rendered  bool
}

func NewPlayer(sentences []string, tty *os.File) *Player {
	return &Player{
		sentences: sentences,
		cmdCh:     make(chan Command, 4),
		tty:       tty,
	}
}

// Run plays sentences sequentially, handling commands from cmdCh.
func (p *Player) Run() {
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
		case len(key) == 1 && key[0] >= '0' && key[0] <= '9':
			p.cmdCh <- CmdSeek + Command(key[0]-'0')
		}
	}
}

const barWidth = 36

// renderUI draws (or redraws) the 7-line status block to stdout.
func (p *Player) renderUI() {
	if p.rendered {
		// Move cursor up 6 lines to overwrite. The controls line has no
		// trailing newline, so the cursor sits on row R+6 after a render;
		// \033[6A returns to row R (the prev-sentence line) exactly.
		fmt.Print("\033[6A")
	}

	termWidth, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || termWidth < 20 {
		termWidth = 80
	}
	// if termWidth > 80 {
	// 	termWidth = 80
	// }

	// Reserve 4 chars for the leading `  "` and trailing `"`.
	maxWidth := termWidth - 4
	if maxWidth < 10 {
		maxWidth = 10
	}

	prev, curr, next := p.contextSentences(maxWidth)

	total := len(p.sentences)
	current := p.index + 1
	filled := barWidth * current / total
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	pct := 100 * current / total

	pauseLabel := ""
	if p.paused {
		pauseLabel = "  [PAUSED]"
	}

	fmt.Printf("\r\033[K\033[2m  \"%s\"\033[0m\r\n", prev)
	fmt.Printf("\r\033[K  \"%s\"\r\n", curr)
	fmt.Printf("\r\033[K\033[2m  \"%s\"\033[0m\r\n", next)
	fmt.Printf("\r\033[K\r\n")
	fmt.Printf("\r\033[K  [%s] %d%% (%d/%d)%s\r\n", bar, pct, current, total, pauseLabel)
	fmt.Printf("\r\033[K\r\n")
	fmt.Printf("\r\033[K  ← prev  [space] pause/resume  → next  q quit")

	p.rendered = true
}

// contextSentences returns the prev, current, and next sentence strings,
// truncated to maxWidth. Empty strings are returned at the boundaries.
func (p *Player) contextSentences(maxWidth int) (prev, curr, next string) {
	if p.index > 0 {
		prev = truncate(p.sentences[p.index-1], maxWidth)
	}
	curr = truncate(p.sentences[p.index], maxWidth)
	if p.index < len(p.sentences)-1 {
		next = truncate(p.sentences[p.index+1], maxWidth)
	}
	return
}

// clearUI moves past the UI block so the shell prompt appears cleanly.
func (p *Player) clearUI() {
	if p.rendered {
		fmt.Print("\r\n")
	}
}

// truncate shortens s to at most n runes, adding "…" if cut.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return strings.ReplaceAll(string(runes[:n-1])+"…", "\n", " ")
}
