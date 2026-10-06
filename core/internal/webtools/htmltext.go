package webtools

import (
	"bytes"
	"html"
	"net/url"
	"strings"
)

// htmlToText turns a web page into the text a model should read: the title, then the body
// as plain lines, with headings marked, list items bulleted and links kept as
// [text](address) so the model can follow one. Scripts, styles and other things that are
// not prose are dropped. It is a tokenizer, not a parser: a web page is routinely broken,
// and the aim is "readable", never "faithful".
func htmlToText(src string, base *url.URL) (title, text string) {
	c := &converter{base: base}
	c.run(src)
	return strings.TrimSpace(html.UnescapeString(c.title.String())), tidy(c.out.String())
}

type converter struct {
	base  *url.URL
	out   bytes.Buffer
	title strings.Builder

	inTitle bool
	// skip counts open elements whose content is not shown (nav-like chrome is kept: a
	// menu can be what the user asked about).
	skip int
	// href is the address of the link being read, "" outside one; linkStart is where its
	// text began in out.
	href      string
	linkStart int
}

// dropped elements: their content is never prose.
var dropped = map[string]bool{
	"script": true, "style": true, "noscript": true, "svg": true, "template": true,
	"iframe": true, "canvas": true, "object": true,
}

// blocks start and end a line.
var blocks = map[string]bool{
	"p": true, "div": true, "section": true, "article": true, "header": true, "footer": true,
	"main": true, "nav": true, "aside": true, "ul": true, "ol": true, "table": true,
	"tr": true, "blockquote": true, "pre": true, "form": true, "figure": true,
	"figcaption": true, "dl": true, "dt": true, "dd": true, "hr": true, "br": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "li": true,
	"details": true, "summary": true, "fieldset": true, "address": true,
}

func (c *converter) run(s string) {
	i := 0
	for i < len(s) {
		lt := strings.IndexByte(s[i:], '<')
		if lt < 0 {
			c.text(s[i:])
			return
		}
		c.text(s[i : i+lt])
		i += lt
		switch {
		case strings.HasPrefix(s[i:], "<!--"):
			end := strings.Index(s[i+4:], "-->")
			if end < 0 {
				return
			}
			i += 4 + end + 3
			continue
		case i+1 < len(s) && (s[i+1] == '!' || s[i+1] == '?'):
			end := strings.IndexByte(s[i:], '>')
			if end < 0 {
				return
			}
			i += end + 1
			continue
		}
		name, attrs, closing, next, ok := readTag(s, i)
		if !ok {
			// A lone '<' is text.
			c.text("<")
			i++
			continue
		}
		i = next
		if !closing && (name == "script" || name == "style") {
			// Raw text: everything up to the matching close tag is code, never markup.
			i = skipRaw(s, i, name)
			continue
		}
		c.tag(name, attrs, closing)
	}
}

// readTag reads the tag starting at s[i] == '<'. next is the index after its '>'.
func readTag(s string, i int) (name, attrs string, closing bool, next int, ok bool) {
	j := i + 1
	if j < len(s) && s[j] == '/' {
		closing = true
		j++
	}
	start := j
	for j < len(s) && (isAlnum(s[j]) || s[j] == '-' || s[j] == ':') {
		j++
	}
	if j == start {
		return "", "", false, 0, false
	}
	name = strings.ToLower(s[start:j])
	// Attributes up to the closing '>', which may sit inside a quoted value.
	attrStart := j
	var quote byte
	for j < len(s) {
		ch := s[j]
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
		} else if ch == '"' || ch == '\'' {
			quote = ch
		} else if ch == '>' {
			return name, s[attrStart:j], closing, j + 1, true
		}
		j++
	}
	// Never closed: the rest of the page is a tag, so there is nothing more to read.
	return name, s[attrStart:], closing, len(s), true
}

func isAlnum(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}

// skipRaw returns the index just past </name> at or after i, or len(s).
func skipRaw(s string, i int, name string) int {
	lower := strings.ToLower(s[i:])
	end := strings.Index(lower, "</"+name)
	if end < 0 {
		return len(s)
	}
	gt := strings.IndexByte(s[i+end:], '>')
	if gt < 0 {
		return len(s)
	}
	return i + end + gt + 1
}

func (c *converter) tag(name, attrs string, closing bool) {
	if dropped[name] {
		if closing {
			if c.skip > 0 {
				c.skip--
			}
		} else {
			c.skip++
		}
		return
	}
	switch name {
	case "title":
		c.inTitle = !closing
		return
	case "a":
		if closing {
			c.endLink()
		} else {
			c.endLink()
			if h := c.resolve(attr(attrs, "href")); h != "" {
				c.href, c.linkStart = h, c.out.Len()
				c.out.WriteByte('[')
			}
		}
		return
	case "img":
		if !closing && c.skip == 0 {
			if alt := strings.TrimSpace(html.UnescapeString(attr(attrs, "alt"))); alt != "" {
				c.out.WriteString("[image: " + alt + "]")
			}
		}
		return
	case "li":
		if !closing {
			c.newline()
			if c.skip == 0 {
				c.out.WriteString("- ")
			}
		}
		return
	}
	if len(name) == 2 && name[0] == 'h' && name[1] >= '1' && name[1] <= '6' {
		c.newline()
		if !closing && c.skip == 0 {
			c.out.WriteString(strings.Repeat("#", int(name[1]-'0')) + " ")
		}
		return
	}
	if blocks[name] {
		c.newline()
	}
}

// resolve makes a link absolute and keeps only the ones worth following.
func (c *converter) resolve(href string) string {
	href = strings.TrimSpace(html.UnescapeString(href))
	if href == "" || strings.HasPrefix(href, "#") {
		return ""
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if c.base != nil {
		u = c.base.ResolveReference(u)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	u.Fragment = ""
	return u.String()
}

func (c *converter) endLink() {
	if c.href == "" {
		return
	}
	// The '[' went out when the link opened; a link with no text leaves nothing behind.
	if strings.TrimSpace(c.out.String()[c.linkStart+1:]) == "" {
		c.out.Truncate(c.linkStart)
	} else {
		c.out.WriteString("](" + c.href + ")")
	}
	c.href = ""
}

func (c *converter) newline() {
	if c.skip > 0 {
		return
	}
	c.out.WriteByte('\n')
}

func (c *converter) text(s string) {
	if s == "" {
		return
	}
	if c.inTitle {
		c.title.WriteString(s)
		return
	}
	if c.skip > 0 {
		return
	}
	c.out.WriteString(html.UnescapeString(s))
}

// attr returns the value of the named attribute in a tag's attribute text.
func attr(attrs, name string) string {
	low := strings.ToLower(attrs)
	for from := 0; ; {
		k := strings.Index(low[from:], name)
		if k < 0 {
			return ""
		}
		k += from
		from = k + len(name)
		if k > 0 && !isSpace(attrs[k-1]) {
			continue
		}
		j := from
		for j < len(attrs) && isSpace(attrs[j]) {
			j++
		}
		if j >= len(attrs) || attrs[j] != '=' {
			continue
		}
		j++
		for j < len(attrs) && isSpace(attrs[j]) {
			j++
		}
		if j >= len(attrs) {
			return ""
		}
		if q := attrs[j]; q == '"' || q == '\'' {
			end := strings.IndexByte(attrs[j+1:], q)
			if end < 0 {
				return attrs[j+1:]
			}
			return attrs[j+1 : j+1+end]
		}
		end := j
		for end < len(attrs) && !isSpace(attrs[end]) {
			end++
		}
		return attrs[j:end]
	}
}

func isSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' }

// tidy squeezes the runs of blanks and blank lines a page leaves behind.
func tidy(s string) string {
	s = strings.ReplaceAll(s, "\u00a0", " ")
	lines := strings.Split(s, "\n")
	var out []string
	blank := 0
	for _, l := range lines {
		l = strings.Join(strings.Fields(l), " ")
		if l == "" || l == "-" {
			blank++
			if blank > 1 || len(out) == 0 {
				continue
			}
			out = append(out, "")
			continue
		}
		blank = 0
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
