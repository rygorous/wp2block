package shortcode

import (
	"code.google.com/p/go.net/html"
	"fmt"
	"strings"
	"unicode/utf8"
)

func ProcessShortcodes(node *html.Node) error {
	if err := processNode(node); err != nil {
		return err
	}

	cleanupTree(node)
	return nil
}

// Is a given shortcode a block or a standalone tag?
// This also serves as master array of shortcode types.
var shortcodeIsBlock = map[string]bool{
	"caption": true,
	"latex":   true,
}

type openTag struct {
	tag  string
	node *html.Node // Node corresponding to this tag
}

func processNode(node *html.Node) (err error) {
	var stackTags [16]openTag
	tags := stackTags[:0]

	n := node.FirstChild
	for n != nil {
		var next, newParent *html.Node

		next = n.NextSibling
		if l := len(tags); l != 0 {
			newParent = tags[l-1].node
		}

		switch n.Type {
		case html.TextNode:
			if tags, next, err = processTextNode(n, tags); err != nil {
				return
			}
		case html.ElementNode:
			if err = processNode(n); err != nil {
				return
			}
		default:
			// Other node types are just ignored.
		}

		// reparent the active node if necessary
		if newParent != nil {
			node.RemoveChild(n)
			newParent.AppendChild(n)
		}

		n = next
	}

	if len(tags) != 0 {
		err = fmt.Errorf("shortcodes still open at end of surrounding HTML tag: %+v", tags)
	}

	return
}

func processTextNode(node *html.Node, tags []openTag) (outTags []openTag, next *html.Node, err error) {
	i := 0
	for i < len(node.Data) {
		r, rsize := utf8.DecodeRuneInString(node.Data[i:])
		switch r {
		case '[':
			size, openClose, tag, rest := parseShortcode(node.Data[i+1:])
			if size != 0 {
				// looks like we found a shortcode!
				if tag == "" { // escape code?
					// remove the outer [] and continue
					node.Data = node.Data[:i] + rest + node.Data[i+1+size:]
					i += len(rest)
				} else {
					return handleShortcode(node, tags, i, i+1+size, openClose, tag, rest)
				}
			} else {
				i += rsize
			}

		default:
			i += rsize
		}
	}

	// default: no shortcode found
	outTags = tags
	next = node.NextSibling
	err = nil
	return
}

func handleShortcode(node *html.Node, tags []openTag, tagStart, tagEnd, openClose int, tag, rest string) (outTags []openTag, next *html.Node, err error) {
	// Split the text node, cutting out the tag
	next = splitTextNode(node, tagStart, tagEnd)

	// On tag open, push new node onto tag stack
	if openClose&tagOpen != 0 {
		tagnode := &html.Node{
			Type:      html.ElementNode,
			Data:      tag,
			Namespace: "wp",
		}
		parseAttrs(tagnode, rest)
		node.Parent.InsertBefore(tagnode, next)

		tags = append(tags, openTag{tag: tag, node: tagnode})
	}

	// On tag close, pop node and verify that it is correctly matched
	if openClose&tagClose != 0 {
		l := len(tags)
		if l == 0 || tags[l-1].tag != tag {
			err = fmt.Errorf("shortcode: unexpected closing shortcode '%s'", tag)
			return
		}
		tags = tags[:l-1]
	}

	outTags = tags
	return
}

// These functions match Perl character classes.
func isspace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r' || ch == '\f'
}

func isword(ch byte) bool {
	return ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '_'
}

func isshortname(ch byte) bool {
	return ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-'
}

const (
	tagOpen = 1 << iota
	tagClose
)

// text is everything *after* the initial '['
func parseShortcode(text string) (size, openClose int, tag, rest string) {
	pos := 0
	startEscape := false
	if len(text) > 0 && text[0] == '[' {
		startEscape = true
		pos = 1
	}

	// is this a closing tag?
	if pos < len(text) && text[pos] == '/' {
		openClose |= tagClose
	} else {
		openClose |= tagOpen
	}

	// scan the tag name
	namestart := pos
	nameend := pos + 1
	for nameend < len(text) && isshortname(text[nameend]) {
		nameend++
	}

	// do we know this shortcode tag?
	tag = text[namestart:nameend]
	if block, ok := shortcodeIsBlock[tag]; !ok {
		// no, stop.
		return
	} else if !block {
		// if it's not a block tag, [/tag] makes no sense.
		if openClose == tagClose {
			return
		}
		openClose = tagOpen | tagClose
	}

	// find closing bracket
	end := strings.Index(text, "]")
	if end < nameend {
		return
	}

	// are we an escaped [[tag]]?
	if startEscape && end == nameend && end+1 < len(text) && text[end+1] == ']' {
		size = end + 2
		openClose = 0
		tag = ""
		rest = text[:end+1]
		return
	}

	// do we end with a closing slash?
	restend := end
	if text[end-1] == '/' {
		openClose |= tagClose
		restend--
	} else if openClose&tagClose != 0 && nameend != restend {
		// Actual closing tags may not have anything but the tag name.
		return
	}

	size = end + 1
	rest = text[nameend:restend]
	return
}

func parseAttrs(node *html.Node, attrs string) {
}

// Splits the html.TextNode "node" into two nodes: one that holds
// Data[:splitBefore], and one that holds Data[splitAfter:]. "node"
// is modified in place to be the first result node; the second node
// is the return value.
func splitTextNode(node *html.Node, splitBefore, splitAfter int) *html.Node {
	newNode := &html.Node{
		Type: html.TextNode,
		Data: node.Data[splitAfter:],
	}
	node.Data = node.Data[:splitBefore]
	node.Parent.InsertBefore(newNode, node.NextSibling)
	return newNode
}

// The splitting process may leave TextNodes with no Data, which we keep
// around to make the data manipulation simpler. This function removes
// them.
func cleanupTree(node *html.Node) {
	var next *html.Node
	for n := node.FirstChild; n != nil; n = next {
		next = n.NextSibling
		switch n.Type {
		case html.TextNode:
			if len(n.Data) == 0 {
				node.RemoveChild(n)
			}
		case html.ElementNode:
			cleanupTree(n)
		default:
			// ignore other node types.
		}
	}
}
