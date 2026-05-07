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
	"net/url"

	"time"
	"golang.org/x/net/html"

	readability "codeberg.org/readeck/go-readability/v2"
	"github.com/PuerkitoBio/goquery"
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
var pronunciations map[string]string
var pronunciationsPath string

func loadPronunciations(path string) map[string]string {
	if path == "" {
		// Try current directory first
		if _, err := os.Stat("pronunciations.txt"); err == nil {
			path = "pronunciations.txt"
		} else {
			exe, err := os.Executable()
			if err != nil {
				return map[string]string{}
			}
			path = filepath.Join(filepath.Dir(exe), "pronunciations.txt")
		}
	}
	f, err := os.Open(path)
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
		fmt.Fprintln(os.Stderr, "Usage: aloud [--at <percent>] [--pronunciations <file>] <file>\n       echo \"text\" | aloud [--at <percent>] [--pronunciations <file>]")
	}
	// flag.Parse() is now called in main

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

func sanitizeFilename(title string) string {
	// Allow only alphanumeric, space, dot, underscore, and dash; limit to 255 chars
	title = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == ' ' || r == '.' || r == '_' || r == '-' {
			return r
		}
		return -1
	}, title)
	title = strings.TrimSpace(title)
	if len(title) > 255 {
		title = title[:255]
	}
	if title == "" {
		title = "article"
	}
	return title
}


func main() {
	// Lock the main goroutine to the OS main thread so the Cocoa main run loop
	// (required by MPRemoteCommandCenter) runs on the correct thread.
	runtime.LockOSThread()

	flag.StringVar(&pronunciationsPath, "pronunciations", "", "path to pronunciations file")
	urlFlag := flag.String("url", "", "URL to fetch text from (plain text or HTML)")
	outputFlag := flag.String("output", "", "Write article text to file or directory (uses article title as filename if directory)")
	atPct := flag.Int("at", 0, "start playback at `percent` (0-100)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: aloud [--at <percent>] [--pronunciations <file>] [--output <file|dir>] <file>\n       echo \"text\" | aloud [--at <percent>] [--pronunciations <file>] [--output <file|dir>]")
	}
	flag.Parse()

	if *atPct < 0 || *atPct > 100 {
		fmt.Fprintln(os.Stderr, "aloud: --at must be between 0 and 100")
		os.Exit(1)
	}

	pronunciations = loadPronunciations(pronunciationsPath)

	// Only one input source allowed: file, stdin, or URL
	fileArg := flag.NArg() == 1
	stdinArg := !term.IsTerminal(int(os.Stdin.Fd()))
	urlArg := *urlFlag != ""
	inputSources := 0
	if fileArg { inputSources++ }
	if stdinArg { inputSources++ }
	if urlArg { inputSources++ }
	if inputSources > 1 {
		fmt.Fprintln(os.Stderr, "aloud: only one input source allowed (file, stdin, or --url)")
		os.Exit(1)
	}

	var input string
	var articleTitle string
	if urlArg {
		article, err := readability.FromURL(*urlFlag, 15*time.Second)
		if err != nil {
			fmt.Fprintf(os.Stderr, "aloud: failed to extract article: %v\n", err)
			os.Exit(1)
		}
		articleTitle = article.Title()
		var sb strings.Builder
		sb.WriteString(articleTitle)
		sb.WriteString("\n\n")
		err = article.RenderText(&sb)
		if err != nil {
			fmt.Fprintf(os.Stderr, "aloud: failed to render article text: %v\n", err)
			os.Exit(1)
		}
		input = strings.TrimSpace(sb.String())
	} else if fileArg {
		data, err := os.ReadFile(flag.Arg(0))
		if err != nil {
			fmt.Fprintf(os.Stderr, "aloud: cannot read file: %v\n", err)
			os.Exit(1)
		}
		if strings.HasSuffix(strings.ToLower(flag.Arg(0)), ".html") || strings.HasPrefix(strings.TrimSpace(string(data)), "<") {
			// Try parsing as a full HTML document, fallback to fragment if needed
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(data)))
			if err != nil {
				// Try parsing as HTML fragment using goquery.NewDocumentFromNode
				node, err2 := html.Parse(strings.NewReader(string(data)))
				if err2 == nil {
					// Find <html> or <body> node for goquery
					var docNode *html.Node
					var findNode func(*html.Node)
					findNode = func(n *html.Node) {
						if n.Type == html.ElementNode && (n.Data == "html" || n.Data == "body") {
							docNode = n
						}
						for c := n.FirstChild; c != nil && docNode == nil; c = c.NextSibling {
							findNode(c)
						}
					}
					findNode(node)
					if docNode != nil {
						doc = goquery.NewDocumentFromNode(docNode)
					}
				}
			}

			if doc != nil {
				absPath, _ := filepath.Abs(flag.Arg(0))
				baseURL, _ := url.Parse("file://" + absPath)
				article, err := readability.FromReader(strings.NewReader(string(data)), baseURL)
				if err != nil {
					fmt.Fprintf(os.Stderr, "aloud: failed to extract article: %v\n", err)
					os.Exit(1)
				}
				articleTitle = article.Title()
				var sb strings.Builder
				sb.WriteString(articleTitle)
				sb.WriteString("\n\n")
				err = article.RenderText(&sb)
				if err != nil {
					fmt.Fprintf(os.Stderr, "aloud: failed to render article text: %v\n", err)
					os.Exit(1)
				}
				input = strings.TrimSpace(sb.String())
			
			} else {
				fmt.Fprintf(os.Stderr, "aloud: goquery returned nil document\n")
				input = string(data)
			}
		} else {
			input = string(data)
		}
	} else if stdinArg {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "aloud: cannot read stdin: %v\n", err)
			os.Exit(1)
		}
		input = string(data)
	} else {
		flag.Usage()
		os.Exit(1)
	}
	input = normalizeASCII(input)

	// Handle --output flag: write article text to file or directory
	if *outputFlag != "" {
		outputPath := *outputFlag
		info, err := os.Stat(outputPath)
		if err == nil && info.IsDir() {
			// Directory: use sanitized article title as filename
			filename := sanitizeFilename(articleTitle)
			outputPath = filepath.Join(outputPath, filename+".md")
		} else if os.IsNotExist(err) && strings.HasSuffix(outputPath, string(os.PathSeparator)) {
			// Directory does not exist, create it
			os.MkdirAll(outputPath, 0755)
			filename := sanitizeFilename(articleTitle)
			outputPath = filepath.Join(outputPath, filename+".md")
		}
		if err := os.WriteFile(outputPath, []byte(input), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "aloud: failed to write output: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "aloud: wrote article to %s\n", outputPath)
		}
	}



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
