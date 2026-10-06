package terminal

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Link is a printed URL or a file reference. Line and Column are 1-based;
// zero means the output did not specify a location.
type Link struct {
	URL, Path    string
	Line, Column int
}

type linkMatch struct {
	Link
	start, end        int // rune indices, excluding surrounding punctuation
	cellA, cellB, row int
}

var pathTokenPattern = regexp.MustCompile(`"[^"\r\n]+"(?::[0-9]+(?::[0-9]+)?)?|'[^'\r\n]+'(?::[0-9]+(?::[0-9]+)?)?|[^\s"'<>` + "`" + `]+`)
var pathLocationPattern = regexp.MustCompile(`:([0-9]+)(?::([0-9]+))?:?$|\(([0-9]+)(?:,([0-9]+))?\)$|#L([0-9]+)(?:C([0-9]+))?$`)

func matchLink(text []rune, cols []int, col int) linkMatch {
	s := string(text)
	contains := func(m linkMatch) bool {
		if m.start < 0 || m.end <= m.start || m.end > len(cols) {
			return false
		}
		last := cols[m.end-1]
		if m.end < len(cols) {
			last = max(last, cols[m.end]-1)
		}
		return cols[m.start] <= col && col <= last
	}
	for _, bounds := range urlPattern.FindAllStringIndex(s, -1) {
		u := trimURL(s[bounds[0]:bounds[1]])
		start := utf8.RuneCountInString(s[:bounds[0]])
		m := linkMatch{Link: Link{URL: u}, start: start, end: start + utf8.RuneCountInString(u)}
		if contains(m) {
			return m
		}
	}
	for _, bounds := range pathTokenPattern.FindAllStringIndex(s, -1) {
		raw := s[bounds[0]:bounds[1]]
		prefix := len(raw) - len(strings.TrimLeft(raw, "([{*"))
		raw = raw[prefix:]
		raw = strings.TrimRight(raw, ",;.!]")
		// A surrounding closing parenthesis is punctuation, but file.ts(4,2)
		// is a compiler location and must be parsed before trimming it.
		if !pathLocationPattern.MatchString(raw) {
			raw = trimURL(raw)
		}
		path := raw
		line, column := 0, 0
		if loc := pathLocationPattern.FindStringSubmatchIndex(raw); loc != nil {
			path = raw[:loc[0]]
			for i := 2; i < len(loc); i += 4 {
				if loc[i] >= 0 {
					line, _ = strconv.Atoi(raw[loc[i]:loc[i+1]])
					if i+3 < len(loc) && loc[i+2] >= 0 {
						column, _ = strconv.Atoi(raw[loc[i+2]:loc[i+3]])
					}
					break
				}
			}
			if line <= 0 {
				continue
			}
		}
		path = strings.Trim(path, "\"'")
		if path == "" || strings.Contains(path, "://") || strings.ContainsAny(path, "\x00\r\n") {
			continue
		}
		// Bare prose is not a path. Extensionless files still work when the
		// output includes a directory or a line location.
		base := path[strings.LastIndex(path, "/")+1:]
		dot := strings.LastIndex(base, ".")
		if !strings.Contains(path, "/") && line == 0 && (dot < 0 || dot == len(base)-1) {
			continue
		}
		if strings.Contains(path, ":") || (strings.Contains(path, " ") && raw[0] != '"' && raw[0] != '\'') {
			continue
		}
		start := utf8.RuneCountInString(s[:bounds[0]+prefix])
		m := linkMatch{Link: Link{Path: path, Line: line, Column: column}, start: start, end: start + utf8.RuneCountInString(raw)}
		if contains(m) {
			return m
		}
	}
	return linkMatch{}
}

func (m linkMatch) valid() bool { return m.URL != "" || m.Path != "" }

// linkAt reads just the pointed logical line, never the full scrollback.
// The emulator lock is held by its caller.
func (v *view) linkAt(x, y float32) linkMatch {
	if v.t.term == nil || v.cellW == 0 || x*v.scale < float32(v.ox) || y*v.scale < float32(v.oy) ||
		x*v.scale >= float32(v.ox+v.cols*v.cellW) || y*v.scale >= float32(v.oy+v.rows*v.cellH) {
		return linkMatch{}
	}
	col, row := v.cellAt(x, y)
	if ref, ok := v.t.term.CellAt(col, row); ok {
		if url := v.t.term.Hyperlink(ref); url != "" {
			a, b := col, col+1
			for a > 0 {
				ref, ok := v.t.term.CellAt(a-1, row)
				if !ok || v.t.term.Hyperlink(ref) != url {
					break
				}
				a--
			}
			for b < v.cols {
				ref, ok := v.t.term.CellAt(b, row)
				if !ok || v.t.term.Hyperlink(ref) != url {
					break
				}
				b++
			}
			return linkMatch{Link: Link{URL: url}, cellA: a, cellB: b, row: row}
		}
	}
	text, cols, start := v.t.term.LogicalRowText(row)
	m := matchLink(text, cols, col+(row-start)*v.cols)
	if m.valid() {
		m.cellA, m.cellB, m.row = cols[m.start], cols[m.end-1]+1, start
		if m.end < len(cols) {
			m.cellB = max(m.cellB, cols[m.end])
		}
	}
	return m
}

// urlPattern finds the URLs programs print as text, which a Command+click
// opens as it does hyperlinks.
var urlPattern = regexp.MustCompile(`(?:https?|ftp|file)://[^\s"'<>` + "`" + `\x00-\x1f]+|mailto:[^\s"'<>` + "`" + `]+`)

// urlAt returns the URL of text, a row whose runes are in columns cols, at
// column col, or "".
func urlAt(text []rune, cols []int, col int) string {
	s := string(text)
	for _, m := range urlPattern.FindAllStringIndex(s, -1) {
		u := trimURL(s[m[0]:m[1]])
		start := len([]rune(s[:m[0]]))
		end := start + len([]rune(u))
		if start < len(cols) && cols[start] <= col && col <= cols[end-1] {
			return u
		}
	}
	return ""
}

// trimURL trims what likely ends the sentence around a URL rather than the
// URL: punctuation, and closing brackets that the URL did not open.
func trimURL(u string) string {
	for len(u) > 0 {
		switch last := u[len(u)-1]; last {
		case '.', ',', ';', ':', '!', '?':
			u = u[:len(u)-1]
			continue
		case ')', ']', '}':
			open := map[byte]string{')': "(", ']': "[", '}': "{"}[last]
			if strings.Count(u, open) < strings.Count(u, string(last)) {
				u = u[:len(u)-1]
				continue
			}
		}
		return u
	}
	return u
}
