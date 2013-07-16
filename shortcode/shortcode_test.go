package shortcode

import (
	"bytes"
	"code.google.com/p/go.net/html"
	"strings"
	"testing"
)

func parseHtmlBody(htmltext string, t *testing.T) *html.Node {
	tree, err := html.Parse(strings.NewReader(htmltext))
	if err != nil {
		t.Errorf("html parse error in %q: %s", htmltext, err.Error())
	} else {
		tree = tree.FirstChild.FirstChild.NextSibling // strip default-inserted html/head nodes, go straight to body
	}

	return tree
}

func renderHtml(tree *html.Node, t *testing.T) string {
	var wr bytes.Buffer
	if err := html.Render(&wr, tree); err != nil {
		t.Errorf("html rendering error: %s", err.Error())
	}
	return wr.String()

}

func TestShortcode(t *testing.T) {
	tests := []struct {
		html, want string
	}{
		{"", "<body></body>"},
		{"a[caption]b[/caption]c", "<body>a<caption>b</caption>c</body>"},
		{"a[caption/]b", "<body>a<caption></caption>b</body>"},
		{"a[caption]b[latex]c[/caption]d[/latex]e", "ERROR"},
		{`a[caption id="b" align='c' width=d]e[/caption]f`, "<body>a<caption id=\"b\" align=\"c\" width=\"d\">e</caption>f</body>"},
		{`a[caption b "c" d="'" e='"' f=g'hi/]j`, `<body>a<caption @0="b" @1="c" d="&#39;" e="&#34;" f="g" @2="&#39;hi"></caption>j</body>`},
		{"a[[caption]]b", "<body>a[caption]b</body>"},
		{"a[thistagisnotdefined]b", "<body>a[thistagisnotdefined]b</body>"},
		{"a[[thistagisnotdefined]]b", "<body>a[[thistagisnotdefined]]b</body>"},
	}
	for _, test := range tests {
		tree := parseHtmlBody(test.html, t)
		err := ProcessShortcodes(tree)
		if err != nil {
			if test.want != "ERROR" {
				t.Errorf("shortcode processing error: %s", err.Error())
			}
		} else {
			got := renderHtml(tree, t)
			if got != test.want {
				t.Errorf("%q: want %q but got %q", test.html, test.want, got)
			}
		}
	}
}

func TestLatex(t *testing.T) {
	tests := []struct {
		html, want string
	}{
		{"", "<body></body>"},
		{"a$latex 1+2$b", "<body>a<latex>1+2</latex>b</body>"},
		{"a $latexb", "<body>a $latexb</body>"},
		{"a $latex b", "<body>a $latex b</body>"},
		{"a $latex b$ c $latex d", "<body>a <latex>b</latex> c $latex d</body>"},
		{"a $latex b$ c $latex d$", "<body>a <latex>b</latex> c <latex>d</latex></body>"},
	}
	for _, test := range tests {
		tree := parseHtmlBody(test.html, t)
		ProcessWpLatex(tree)
		got := renderHtml(tree, t)
		if got != test.want {
			t.Errorf("%q: want %q but got %q", test.html, test.want, got)
		}
	}
}
