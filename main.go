package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"

	"golang.org/x/term"
)

var splitRe = regexp.MustCompile(`[^.][^A-Z][.!?]["'\)]?[\n ]`)

var urlRe = regexp.MustCompile(`https?://(?:www\.)?([a-zA-Z0-9-]+)\.[a-zA-Z]{2,}[^\s]*`)

var asciiNormalizer = strings.NewReplacer(
	"\u201C", `"`, // left double quotation mark
	"\u201D", `"`, // right double quotation mark
	"\u2018", "'", // left single quotation mark
	"\u2019", "'", // right single quotation mark
	"\u2014", "--", // em dash
	"\u2013", "-", // en dash
	"\u2026", "...", // horizontal ellipsis
	"\u00A0", " ", // non-breaking space
)

func normalizeASCII(s string) string { return asciiNormalizer.Replace(s) }

// pronunciations maps words/phrases that `say` mispronounces to better
// alternatives. Replacements are applied to the spoken text only; the
// original text is still shown in the progress bar.
var pronunciations = loadPronunciations()

func loadPronunciations() map[string]string {
	exe, err := os.Executable()
	if err != nil {
		return map[string]string{}
	}
	f, err := os.Open(filepath.Join(filepath.Dir(exe), "pronunciations.txt"))
	if err != nil {
		return map[string]string{}
	}
	defer f.Close()
	m := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key := strings.ReplaceAll(k, `\n`, "\n")
		m[key] = v
	}
	return m
}

func applyPronunciations(s string) string {
	s = urlRe.ReplaceAllStringFunc(s, func(match string) string {
		sub := urlRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		return sub[1] + " URL"
	})
	pairs := make([]string, 0, len(pronunciations)*2)
	for from, to := range pronunciations {
		pairs = append(pairs, from, to)
	}
	return strings.NewReplacer(pairs...).Replace(s)
}

const (
	pauseSentence  = 400
	pauseLine      = 700
	pauseParagraph = 1200
)

type Sentence struct {
	Text  string
	Pause int // ms of silence after this sentence
}

func splitSentences(text string) []Sentence {
	var result []Sentence
	for _, para := range strings.Split(text, "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		lines := strings.Split(para, "\n")
		for li, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			strs := splitLine(line)
			isLastLine := li == len(lines)-1
			for si, s := range strs {
				pause := pauseSentence
				if si == len(strs)-1 {
					if isLastLine {
						pause = pauseParagraph
					} else {
						pause = pauseLine
					}
				}
				result = append(result, Sentence{Text: s, Pause: pause})
			}
		}
	}
	return result
}

func splitLine(text string) []string {
	locs := splitRe.FindAllStringIndex(text, -1)
	if len(locs) == 0 {
		if s := strings.TrimSpace(text); s != "" {
			return []string{s}
		}
		return nil
	}

	var result []string
	pos := 0
	for _, loc := range locs {
		if s := strings.TrimSpace(text[pos:loc[1]]); s != "" {
			result = append(result, s)
		}
		pos = loc[1]
	}
	if s := strings.TrimSpace(text[pos:]); s != "" {
		result = append(result, s)
	}
	return result
}

func getAt() *int {
	atPct := flag.Int("at", 0, "start playback at `percent` (0-100)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: aloud [--at <percent>] <file>\n       echo \"text\" | aloud [--at <percent>]")
	}
	flag.Parse()

	if *atPct < 0 || *atPct > 100 {
		fmt.Fprintln(os.Stderr, "aloud: --at must be between 0 and 100")
		os.Exit(1)
	}
	return atPct

}

func getText() string {
	var input string

	switch {
	case flag.NArg() == 1:
		data, err := os.ReadFile(flag.Arg(0))
		if err != nil {
			fmt.Fprintf(os.Stderr, "aloud: cannot read file: %v\n", err)
			os.Exit(1)
		}
		input = string(data)
	case !term.IsTerminal(int(os.Stdin.Fd())):
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "aloud: cannot read stdin: %v\n", err)
			os.Exit(1)
		}
		input = string(data)
	default:
		flag.Usage()
		os.Exit(1)
	}
	return input
}

func maybeCrash(err error, msg string) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "aloud: %s: %v\n", msg, err)
		os.Exit(1)
	}
}

func main() {
	// Lock the main goroutine to the OS main thread so the Cocoa main run loop
	// (required by MPRemoteCommandCenter) runs on the correct thread.
	runtime.LockOSThread()

	atPct := getAt()
	input := normalizeASCII(getText())
	sentences := splitSentences((input))

	if len(sentences) == 0 {
		fmt.Fprintln(os.Stderr, "aloud: no text found")
		os.Exit(1)
	}

	tty, err := os.Open("/dev/tty")
	maybeCrash(err, "cannot open /dev/tty")

	oldState, err := term.MakeRaw(int(tty.Fd()))
	maybeCrash(err, "cannot set raw terminal")

	restore := func() {
		term.Restore(int(tty.Fd()), oldState)
		tty.Close()
	}

	// Restore terminal on external signals.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		restore()
		os.Exit(0)
	}()

	title := "stdin"
	if flag.NArg() == 1 {
		base := filepath.Base(flag.Arg(0))
		title = strings.TrimSuffix(base, filepath.Ext(base))
	}

	p := NewPlayer(sentences, tty)
	p.index = *atPct * len(sentences) / 100
	startMediaKeyMonitor(p.cmdCh, title)
	go p.keyboardLoop()
	go func() {
		p.Run()
		fmt.Print("\n")
		restore()
		os.Exit(0)
	}()

	// Block the OS main thread on the Cocoa run loop so MPRemoteCommandCenter
	// can receive media key events. Player runs on a separate goroutine above.
	runMainLoop()
}
