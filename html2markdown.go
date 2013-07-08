package main

import (
	"bytes"
	"code.google.com/p/go.net/html"
	"code.google.com/p/go.net/html/atom"
	"errors"
	"fmt"
	"strings"
)

func ConvertHtmlToMarkdown(in []byte) ([]byte, error) {
	// parse it!
	context := &html.Node{
		Type:     html.ElementNode,
		DataAtom: atom.Body,
		Data:     "body",
	}

	reader := bytes.NewReader(in)
	elems, err := html.ParseFragment(reader, context)
	if err != nil {
		return nil, err
	}
	if reader.Len() != 0 {
		return nil, errors.New("Post couldn't be fully parsed!")
	}

	// render it back
	var wr writer
	for _, elem := range elems {
		err = renderElement(&wr, elem, -1)
		if err != nil {
			return nil, err
		}
	}

	return wr.Bytes(), nil
}

type writer struct {
	Verbatim int // if >0, don't do any processing on output newlines

	out     bytes.Buffer
	indents []string // stack of indenting prefixes
}

func (w *writer) Bytes() []byte {
	return w.out.Bytes()
}

func (w *writer) Write(p []byte) (n int, err error) {
	var wr int
	n = 0
	i := bytes.IndexByte(p, '\n')
	for i != -1 {
		wr, err = w.out.Write(p[:i])
		n += wr
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
	wr, err = w.out.Write(p)
	n += wr
	return
}

func (w *writer) WriteByte(b byte) error {
	err := w.out.WriteByte(b)
	if err == nil && b == '\n' && w.Verbatim == 0 {
		// it's a newline we have to process
		if l := len(w.indents); l > 0 {
			_, err = w.out.WriteString(w.indents[l-1])
		}
	}
	return err
}

func (w *writer) WriteString(s string) (n int, err error) {
	n, err = w.Write([]byte(s))
	return
}

func (w *writer) PushIndent(prefix string) {
	if l := len(w.indents); l > 0 {
		prefix = w.indents[l-1] + prefix
	}
	w.indents = append(w.indents, prefix)
}

func (w *writer) PopIndent() {
	w.indents = w.indents[:len(w.indents)-1]
}

var escapedChars = "\\`*_{}[]()#+-.!:|&<>$"

func markdownEscape(w *writer, s string) error {
	// could do a better job here, but this way is safe.
	var last byte
	i := strings.IndexAny(s, escapedChars)
	for i != -1 {
		if _, err := w.WriteString(s[:i]); err != nil {
			return err
		}
		escape := true

		// Detect some easy cases where escapes are both
		// unnecessary and jarring.
		prev := last
		if i > 0 {
			prev = s[i-1]
		}
		switch s[i] {
		case '.':
			// '.' can be magic if it occurs are a digit (might start a list)
			if '0' < prev || '9' > prev {
				escape = false
			}
		case ':':
			// ':' can be magic for autolinking, but only if it's followed by //
			if len(s) < i+3 || s[i+1] != '/' || s[i+2] != '/' {
				escape = false
			}
		case '!':
			// '!' can be magic for image links (when followed by '[')
			if len(s) < i+2 || s[i+1] != '!' {
				escape = false
			}
		}

		if escape {
			if err := w.WriteByte('\\'); err != nil {
				return err
			}
		}
		if err := w.WriteByte(s[i]); err != nil {
			return err
		}
		last = s[i]
		s = s[i+1:]
		i = strings.IndexAny(s, escapedChars)
	}
	_, err := w.WriteString(s)
	return err
}

func surround(w *writer, prefix string, buf []byte, suffix string, escape bool) error {
	if _, err := w.WriteString(prefix); err != nil {
		return err
	}
	var err error
	if escape {
		err = markdownEscape(w, string(buf))
	} else {
		_, err = w.Write(buf)
	}
	if err != nil {
		return err
	}
	_, err = w.WriteString(suffix)
	return err
}

func nowrap(w *writer, prefix string, buf []byte, suffix string) error {
	if _, err := w.WriteString(prefix); err != nil {
		return err
	}
	idx := bytes.IndexAny(buf, "\r\n")
	for idx != -1 {
		if _, err := w.Write(buf[:idx]); err != nil {
			return err
		}
		if err := w.WriteByte(' '); err != nil {
			return err
		}
		buf = buf[idx+1:]
		idx = bytes.IndexAny(buf, "\r\n")
	}
	if _, err := w.Write(buf); err != nil {
		return err
	}
	_, err := w.WriteString(suffix)
	return err
}

func renderElement(w *writer, n *html.Node, listIndex int) error {
	switch n.Type {
	case html.ErrorNode:
		return errors.New("html2markdown: Markup contains errors.")
	case html.TextNode:
		return markdownEscape(w, n.Data)
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
	case atom.H2:
		if t, ok := childText(n); ok {
			return nowrap(w, "\n## ", t, "\n")
		}
	case atom.H3:
		if t, ok := childText(n); ok {
			return nowrap(w, "\n### ", t, "\n")
		}
	case atom.H4:
		if t, ok := childText(n); ok {
			return nowrap(w, "\n#### ", t, "\n")
		}
	case atom.Em, atom.I:
		return renderContents(w, "*", n, "*")
	case atom.Strong, atom.B:
		return renderContents(w, "**", n, "**")
	case atom.Code:
		if contents := tryLeafChildText(n); contents != nil {
			if bytes.IndexByte(contents, '`') == -1 {
				w.Verbatim++
				err := surround(w, "`", contents, "`", false)
				w.Verbatim--
				return err
			}
		}
	case atom.Pre:
		if contents := tryLeafChildText(n); contents != nil {
			if bytes.Index(contents, []byte("```")) == -1 {
				contents := bytes.TrimSuffix(contents, []byte("\n"))
				w.Verbatim++
				err := surround(w, "\n```\n", contents, "\n```\n", false)
				w.Verbatim--
				return err
			}
		}
	case atom.A:
		if isSimpleLink(n) {
			text := leafChildText(n)
			href := attr(n, "href")
			if bytes.IndexAny(text, "[]") == -1 && strings.IndexAny(href, "()") == -1 {
				if err := surround(w, "[", text, "]", true); err != nil {
					return err
				}
				return surround(w, "(", []byte(href), ")", false)
			}
		}
	case atom.Ol, atom.Ul:
		if containsOnlyListItems(n) {
			if err := w.WriteByte('\n'); err != nil {
				return err
			}

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
			if err := renderContents(w, prefix, n, ""); err != nil {
				return err
			}
			w.PopIndent()
			return w.WriteByte('\n')
		}
	}

	// By default, fall back to rendering as HTML
	w.Verbatim++
	err := html.Render(w, n)
	w.Verbatim--
	return err
}

func renderContents(w *writer, prefix string, node *html.Node, suffix string) error {
	if _, err := w.WriteString(prefix); err != nil {
		return err
	}
	for n := node.FirstChild; n != nil; n = n.NextSibling {
		if err := renderElement(w, n, -1); err != nil {
			return err
		}
	}
	_, err := w.WriteString(suffix)
	return err
}

func childText(node *html.Node) ([]byte, bool) {
	var wr writer
	err := renderContents(&wr, "", node, "")
	return wr.Bytes(), err == nil
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

func attr(node *html.Node, key string) string {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}