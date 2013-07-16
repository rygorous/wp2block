package shortcode

import (
	"bytes"
	"code.google.com/p/go.net/html"
	"strings"
	"testing"
)

func TestProcess(t *testing.T) {
	tests := []struct {
		html, want string
	}{
		{"", "<body></body>"},
		{"a[caption]b[/caption]c", "<body>a<caption>b</caption>c</body>"},
		{"a[caption/]b", "<body>a<caption></caption>b</body>"},
		{"a[caption]b[latex]c[/caption]d[/latex]e", "ERROR"},
		{`a[caption id="b" align='c' width=d]e[/caption]f`, "<body>a<caption id=\"b\" align=\"c\" width=\"d\">e</caption>f</body>"},
		{`a[caption b "c" d="'" e='"' f=g'hi/]j`, `<body>a<caption @0="b" @1="c" d="&#39;" e="&#34;" f="g" @2="&#39;hi"></caption>j</body>`},
	}
	for _, test := range tests {
		tree, err := html.Parse(strings.NewReader(test.html))
		if err != nil {
			t.Errorf("html parse error in %q: %s", test.html, err.Error())
		}
		err = ProcessShortcodes(tree)
		if err != nil {
			if test.want != "ERROR" {
				t.Errorf("shortcode processing error: %s", err.Error())
			}
		} else {
			tree = tree.FirstChild.FirstChild.NextSibling // strip default-inserted html/head nodes, go straight to body

			var wr bytes.Buffer
			err = html.Render(&wr, tree)
			if err != nil {
				t.Errorf("html rendering error: %s", err.Error())
			}
			if wr.String() != test.want {
				t.Errorf("%q: want %q but got %q", test.html, test.want, wr.String())
			}
		}
	}
}
