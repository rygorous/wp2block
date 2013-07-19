package main

import (
	"bytes"
	"code.google.com/p/go.net/html"
	"code.google.com/p/go.net/html/atom"
	"errors"
	"fmt"
	"github.com/rygorous/wp2block/shortcode"
	"regexp"
	"strings"
)

type UrlRewriteFunc func(url string) string

func ConvertHtmlToMarkdown(in []byte, rewriteFn UrlRewriteFunc) ([]byte, error) {
	// parse it!
	body := &html.Node{
		Type:     html.ElementNode,
		DataAtom: atom.Body,
		Data:     "body",
	}

	reader := bytes.NewReader(in)
	elems, err := html.ParseFragment(reader, body)
	if err != nil {
		return nil, err
	}
	if reader.Len() != 0 {
		return nil, errors.New("Post couldn't be fully parsed!")
	}

	// stuff it all into the body node so we have a proper tree.
	for _, elem := range elems {
		body.AppendChild(elem)
	}

	// process shortcodes and WP-LaTeX markup.
	if err = shortcode.ProcessShortcodes(body); err != nil {
		return nil, err
	}
	shortcode.ProcessWpLatex(body)

	// render it back
	wr := &writer{RewriteUrl: rewriteFn}
	for elem := body.FirstChild; elem != nil; elem = elem.NextSibling {
		err = renderElement(wr, elem, -1)
		if err != nil {
			return nil, err
		}
	}
	wr.handleDelayedLf()

	return wr.Bytes(), nil
}

type writer struct {
	Verbatim   int // if >0, don't do any processing on output newlines
	RewriteUrl UrlRewriteFunc

	lfRunCounter int // length of the current run of line feeds written
	lfRunTarget  int // target length of current run of line feeds
	out          bytes.Buffer
	indents      []string // stack of indenting prefixes
}

func (w *writer) Bytes() []byte {
	return w.out.Bytes()
}

func (w *writer) String() string {
	return w.out.String()
}

func (w *writer) Clone() *writer {
	return &writer{RewriteUrl: w.RewriteUrl}
}

func (w *writer) handleDelayedLf() {
	for w.lfRunCounter < w.lfRunTarget {
		w.WriteByte('\n')
	}
	w.lfRunTarget = 0
}

func (w *writer) Write(p []byte) (n int, err error) {
	var wr int
	n = 0
	i := bytes.IndexByte(p, '\n')
	for i != -1 {
		if i != 0 {
			w.handleDelayedLf()
		}
		wr, err = w.out.Write(p[:i])
		if wr != 0 {
			n += wr
			w.lfRunCounter = 0
		}
		if err != nil {
			return
		}
		if err = w.WriteByte('\n'); err != nil {
			return
		}
		n++

		p = p[i+1:]
		i = bytes.IndexByte(p, '\n')
	}
	if len(p) != 0 {
		w.handleDelayedLf()
	}
	wr, err = w.out.Write(p)
	if wr != 0 {
		n += wr
		w.lfRunCounter = 0
	}
	return
}

func (w *writer) WriteByte(b byte) error {
	if b != '\n' && w.lfRunTarget != 0 {
		w.handleDelayedLf()
	}
	err := w.out.WriteByte(b)
	if err == nil {
		if b == '\n' {
			w.lfRunCounter++
			// is it a newline we have to process?
			if l := len(w.indents); l > 0 && w.Verbatim == 0 {
				_, err = w.out.WriteString(w.indents[l-1])
			}
		} else {
			w.lfRunCounter = 0
		}
	}
	return err
}

func (w *writer) WriteString(s string) (n int, err error) {
	n, err = w.Write([]byte(s))
	return
}

func (w *writer) EnsureLinefeeds(min int) {
	if min > w.lfRunTarget {
		w.lfRunTarget = min
	}
}

func (w *writer) PushIndent(prefix string) {
	// have to flush linefeed runs here, because we're about to change
	// what happens on linefeed!
	w.handleDelayedLf()
	if l := len(w.indents); l > 0 {
		prefix = w.indents[l-1] + prefix
	}
	w.indents = append(w.indents, prefix)
}

func (w *writer) PopIndent() {
	w.indents = w.indents[:len(w.indents)-1]
}

var escapedCharsAll = "\\`*_{}[]()#+-.!:|&<>$"

func markdownEscape(w *writer, b []byte, escapedChars string) {
	// could do a better job here, but this way is safe.
	var last byte
	i := bytes.IndexAny(b, escapedChars)
	for i != -1 {
		w.Write(b[:i])
		escape := true

		// Detect some easy cases where escapes are both
		// unnecessary and jarring.
		prev := last
		if i > 0 {
			prev = b[i-1]
		}
		switch b[i] {
		case '.':
			// '.' can be magic if it occurs are a digit (might start a list)
			if '0' < prev || '9' > prev {
				escape = false
			}
		case ':':
			// ':' can be magic for autolinking, but only if it's followed by //
			if len(b) < i+3 || b[i+1] != '/' || b[i+2] != '/' {
				escape = false
			}
		case '!':
			// '!' can be magic for image links (when followed by '[')
			if len(b) < i+2 || b[i+1] != '!' {
				escape = false
			}
		}

		if escape {
			w.WriteByte('\\')
		}
		w.WriteByte(b[i])
		last = b[i]
		b = b[i+1:]
		i = bytes.IndexAny(b, escapedChars)
	}
	w.Write(b)
}

func surround(w *writer, prefix string, what []byte, suffix string, escapedChars string) {
	w.WriteString(prefix)
	markdownEscape(w, what, escapedChars)
	w.WriteString(suffix)
}

func singleline(w *writer, prefix string, buf []byte) {
	w.WriteString(prefix)
	idx := bytes.IndexAny(buf, "\r\n")
	for idx != -1 {
		w.Write(buf[:idx])
		w.WriteByte(' ')
		buf = buf[idx+1:]
		idx = bytes.IndexAny(buf, "\r\n")
	}
	w.Write(buf)
}

var headingPrefix = [...]string{
	"# ",
	"## ",
	"### ",
	"#### ",
	"##### ",
	"###### ",
}

func renderElement(w *writer, n *html.Node, listIndex int) error {
	switch n.Type {
	case html.ErrorNode:
		return errors.New("html2markdown: Markup contains errors.")
	case html.TextNode:
		text := n.Data
		if isPrevBlockBoundary(n) {
			text = trimLeftNewline(text)
		}
		if isNextBlockBoundary(n) {
			text = trimRightNewline(text)
		}
		return handleText(w, text)
	case html.ElementNode:
		// nothing.
	case html.CommentNode:
		if n.Data == "more" {
			// the split marker we potentially care about
		}
		return nil
	case html.DoctypeNode:
		return errors.New("html2markdown: Markup isn't supposed to contain a Doctype declaration.")
	default:
		return errors.New("html2markdown: unknown node type")
	}

	switch n.DataAtom {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		level := int(n.DataAtom.String()[1] - '1')
		if t, ok := childText(w, n); ok {
			w.EnsureLinefeeds(2)
			singleline(w, headingPrefix[level], t)
			w.EnsureLinefeeds(2)
			return nil
		}
	case atom.Em, atom.I:
		return renderContents(w, "*", n, "*")
	case atom.Strong, atom.B:
		return renderContents(w, "**", n, "**")
	case atom.Code:
		if contents := tryLeafChildText(n); contents != nil {
			if bytes.IndexByte(contents, '`') == -1 {
				w.Verbatim++
				surround(w, "`", contents, "`", "")
				w.Verbatim--
				return nil
			}
		}
	case atom.Pre:
		if contents := tryLeafChildText(n); contents != nil {
			if bytes.Index(contents, []byte("```")) == -1 {
				contents = tabsToSpaces(contents, 8)
				w.EnsureLinefeeds(2)
				w.WriteString("```\n")
				w.Verbatim++
				surround(w, "", contents, "", "")
				w.Verbatim--
				w.EnsureLinefeeds(1)
				w.WriteString("```")
				w.EnsureLinefeeds(2)
				return nil
			}
		}
	case atom.A:
		if isSimpleLink(n) {
			text := leafChildText(n)
			href := attr(n, "href")
			href = w.RewriteUrl(href)
			surround(w, "[", text, "]", "[]")
			surround(w, "(", []byte(href), ")", "()")
			return nil
		} else if isImageLink(n) && handleImage(w, n.FirstChild) {
			return nil
		}
	case atom.Img:
		if handleImage(w, n) {
			return nil
		}
	case atom.Ol, atom.Ul:
		if containsOnlyListItems(n) {
			w.EnsureLinefeeds(2)
			i := 0
			for kid := n.FirstChild; kid != nil; kid = kid.NextSibling {
				if kid.Type != html.ElementNode {
					continue
				}

				if err := renderElement(w, kid, i); err != nil {
					return err
				}
				i++
			}
			w.EnsureLinefeeds(2)

			return nil
		}
	case atom.Li:
		var parentAtom atom.Atom
		if n.Parent != nil {
			parentAtom = n.Parent.DataAtom
		}
		if listIndex >= 0 && (parentAtom == atom.Ol || parentAtom == atom.Ul) {
			var prefix string
			if parentAtom == atom.Ol {
				prefix = fmt.Sprintf("%d. ", listIndex+1)
			} else if parentAtom == atom.Ul {
				prefix = "* "
			}
			w.PushIndent("    ")
			err := renderContents(w, prefix, n, "")
			w.PopIndent()
			w.EnsureLinefeeds(1)
			return err
		}
	case atom.Blockquote:
		w.EnsureLinefeeds(2)
		w.PushIndent("> ")
		err := renderContents(w, "> ", n, "")
		w.PopIndent()
		w.EnsureLinefeeds(1)
		return err
	}

	if n.Namespace == shortcode.Namespace {
		switch n.Data {
		case "latex":
			text := leafChildText(n)
			start := "$$"
			if len(text) > 0 && text[0] == '[' {
				// don't want to accidentally produce a $$[
				start = "$$ "
			}
			surround(w, start, text, "$$", "")
			return nil
		case "caption", "wp_caption":
			return handleWpCaption(w, n)
		default:
			return fmt.Errorf("unhandled shortcode %q", n.Data)
		}
	}

	// By default, fall back to rendering as HTML
	w.Verbatim++
	err := html.Render(w, n)
	w.Verbatim--
	return err
}

func renderContents(w *writer, prefix string, node *html.Node, suffix string) error {
	w.WriteString(prefix)
	for n := node.FirstChild; n != nil; n = n.NextSibling {
		if err := renderElement(w, n, -1); err != nil {
			return err
		}
	}
	w.WriteString(suffix)
	return nil
}

func childText(w *writer, node *html.Node) ([]byte, bool) {
	wr := w.Clone()
	err := renderContents(wr, "", node, "")
	return wr.Bytes(), err == nil
}

func handleText(w *writer, text string) error {
	// Handle line breaks:
	// single \n -> <br>
	// at least two: -> paragraph break. (\n\n in Markdown)
	i := strings.Index(text, "\n")
	for i != -1 {
		// handle bit up to newline
		markdownEscape(w, []byte(text[:i]), escapedCharsAll)

		// figure out the end of this run of newlines
		end := i + 1
		for end < len(text) && text[end] == '\n' {
			end++
		}

		if end == i+1 {
			// single line break
			w.WriteString("\n<br>")
		} else {
			// paragraph break
			w.WriteString("\n\n")
		}

		text = text[end:]
		i = strings.Index(text, "\n")
	}

	markdownEscape(w, []byte(text), escapedCharsAll)
	return nil
}

func handleWpCaption(w *writer, node *html.Node) error {
	if err := checkWpCaption(node); err != nil {
		return err
	}

	var renderEnd *html.Node
	var caption string
	if !hasAttr(node, "caption") {
		// New-style caption - render contents to string
		wr := w.Clone()
		renderEnd = node.FirstChild.NextSibling
		for n := renderEnd; n != nil; n = n.NextSibling {
			if err := renderElement(wr, n, -1); err != nil {
				return err
			}
		}
		caption = strings.TrimSpace(wr.String())
	} else {
		// Old-style caption is an attribute
		caption = attr(node, "caption")
	}

	// TODO handle other attributes!
	w.WriteString("{% figure %}")
	for n := node.FirstChild; n != renderEnd; n = n.NextSibling {
		if err := renderContents(w, "", n, ""); err != nil {
			return err
		}
	}
	w.WriteString("{% figcaption %}")
	w.WriteString(caption)
	w.WriteString("{% endfigcaption %}{% endfigure %}")

	return nil
}

func checkWpCaption(node *html.Node) error {
	kid := node.FirstChild
	if kid != nil && kid.Type == html.ElementNode && kid.DataAtom == atom.A {
		// Links, we may descend.
		kid = kid.FirstChild
	}
	if kid == nil || kid.Type != html.ElementNode || kid.DataAtom != atom.Img {
		return errors.New("The content of Wordpress caption shortcodes must start with an image, possibly nested inside a link.")
	}

	return nil
}

var (
	imgAllowedAttrs = map[string]bool{
		"src":    true,
		"alt":    true,
		"title":  true,
		"width":  true,
		"height": true,
		"class":  true,
		"style":  true,
	}
	hasFloatLeft  = regexp.MustCompile(`(\W|^)float:\s*left\s*(;|$)`)
	hasFloatRight = regexp.MustCompile(`(\W|^)float:\s*right\s(;|$)`)
)

func handleImage(w *writer, node *html.Node) bool {
	if !hasOnlyAllowedAttrs(node, imgAllowedAttrs) {
		return false
	}

	url := attr(node, "src")
	alt := attr(node, "alt")
	title := attr(node, "title")

	url = w.RewriteUrl(url)

	// TODO look at class for alignment
	out_attrs := ""
	if style := attr(node, "style"); style != "" {
		if hasFloatLeft.MatchString(style) {
			out_attrs += " floatleft"
		}
		if hasFloatRight.MatchString(style) {
			out_attrs += " floatright"
		}
	}

	if out_attrs != "" {
		alt = "{" + strings.TrimSpace(out_attrs) + "}" + alt
	}

	surround(w, "![", []byte(alt), "]", "[]")
	if title == "" {
		surround(w, "(", []byte(url), ")", "()")
	} else {
		surround(w, "(", []byte(url), " ", "\"()")
		surround(w, "\"", []byte(title), "\")", "\"")
	}
	return true
}

// Returns whether a node contains any markup whatsoever
func containsMarkup(node *html.Node) bool {
	if node.FirstChild == node.LastChild {
		if node.FirstChild == nil || node.FirstChild.Type == html.TextNode {
			return false
		}
	}
	return true
}

// Returns whether a node contains only "li" elements as children
// (but allow text nodes if they're only white space)
func containsOnlyListItems(node *html.Node) bool {
	for n := node.FirstChild; n != nil; n = n.NextSibling {
		switch n.Type {
		case html.TextNode:
			if strings.TrimSpace(n.Data) != "" {
				return false
			}
		case html.ElementNode:
			if n.DataAtom != atom.Li {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// Gets the child text, but only if the node doesn't contain any other nodes
// or attributes.
func tryLeafChildText(node *html.Node) []byte {
	if len(node.Attr) == 0 && !containsMarkup(node) {
		return leafChildText(node)
	}
	return nil
}

// Gets the child text, but only if the node doesn't contain any other nodes
// or attributes.
func leafChildText(node *html.Node) []byte {
	if node.FirstChild == nil {
		return []byte("")
	} else if node.FirstChild.Type == html.TextNode {
		return []byte(node.FirstChild.Data)
	}

	return nil
}

// Returns whether a link is "simple", i.e. just has a href and nothing else.
func isSimpleLink(node *html.Node) bool {
	return !containsMarkup(node) && len(node.Attr) == 1 && node.Attr[0].Key == "href"
}

func isImageLink(node *html.Node) bool {
	// Actual link must have only an href attribute
	if len(node.Attr) != 1 || node.Attr[0].Key != "href" {
		return false
	}

	// It most contain exactly one element.
	if node.FirstChild == nil || node.FirstChild != node.LastChild {
		return false
	}

	// That element must be an img tag.
	imgTag := node.FirstChild
	if imgTag.Type != html.ElementNode || imgTag.DataAtom != atom.Img {
		return false
	}

	// Image source and link href must be the same value
	if attr(node, "href") != attr(imgTag, "src") {
		return false
	}

	return true
}

// Checks whether all attributes of "node" are in "allowed"
func hasOnlyAllowedAttrs(node *html.Node, allowed map[string]bool) bool {
	for _, attr := range node.Attr {
		if !allowed[attr.Key] {
			return false
		}
	}
	return true
}

func hasAttr(node *html.Node, key string) bool {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return true
		}
	}
	return false
}

func attr(node *html.Node, key string) string {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func isBlockLevelElement(node *html.Node) bool {
	if node.Type != html.ElementNode {
		return false
	}

	switch node.DataAtom {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		return true
	case atom.Div, atom.P, atom.Hr, atom.Blockquote, atom.Pre:
		return true
	case atom.Ol, atom.Ul, atom.Dl, atom.Dd:
		return true
	case atom.Form:
		return true
	}

	return false
}

func isPrevBlockBoundary(node *html.Node) bool {
	n := node

	// walk up to the first parent that has a prev sibling
	// if we run into block-level elements along the way,
	// we've crossed a boundary!
	for {
		if isBlockLevelElement(n) {
			return true
		}
		if n.PrevSibling != nil {
			n = n.PrevSibling
			break
		}
		n = n.Parent
		if n == nil {
			return true
		}
	}

	// walk down to the leaves and check again if we run
	// into block-level elements.
	for n != nil {
		if isBlockLevelElement(n) {
			return true
		}
		n = n.LastChild
	}

	return false
}

func isNextBlockBoundary(node *html.Node) bool {
	n := node

	// dual to the above function
	for {
		if isBlockLevelElement(n) {
			return true
		}
		if n.NextSibling != nil {
			n = n.NextSibling
			break
		}
		n = n.Parent
		if n == nil {
			return true
		}
	}

	for n != nil {
		if isBlockLevelElement(n) {
			return true
		}
		n = n.FirstChild
	}

	return false
}

// Strips a newline at the start, but only if it's a single one.
func trimLeftNewline(text string) string {
	l := len(text)
	if l >= 1 && text[0] == '\n' && (l < 2 || text[1] != '\n') {
		return text[1:]
	}
	return text
}

// Strips a newline at the end, but only if it's a single one.
func trimRightNewline(text string) string {
	l := len(text)
	if l >= 1 && text[l-1] == '\n' && (l < 2 || text[l-2] != '\n') {
		return text[:l-1]
	}
	return text
}

func tabsToSpaces(in []byte, tabsize int) []byte {
	if bytes.IndexByte(in, '\t') == -1 {
		return in
	}

	spaces := bytes.Repeat([]byte(" "), tabsize)

	var out []byte
	i := bytes.IndexAny(in, "\n\r\f\t")
	col := 0

	for i != -1 {
		out = append(out, in[:i]...)
		col += utf8.RuneCount(in[:i])
		if in[i] == '\t' {
			nspaces := tabsize - (col % tabsize)
			out = append(out, spaces[:nspaces]...)
			col += nspaces
		} else {
			// line feed
			out = append(out, in[i])
			col = 0
		}

		in = in[i+1:]
		i = bytes.IndexAny(in, "\n\r\f\t")
	}

	return append(out, in...)
}
