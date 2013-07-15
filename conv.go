// This program takes a Wordpress export XML and converts it to "Block"-style
// blog posts.
package main

import (
	"encoding/xml"
	"fmt"
	"github.com/rygorous/wp2block/wxr"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type CommentType int
type DocType int
type DocStatus int

const (
	CommentRegular CommentType = iota
	CommentPingback

	DocPost DocType = iota
	DocPage

	StatusPublish DocStatus = iota
	StatusDraft
	StatusPending
	StatusPrivate
)

type Blog struct {
	Author Author
	Docs   []*Doc
}

type Author struct {
	Name  string
	Email string
}

type Doc struct {
	Id              string
	Title           string
	Content         []byte
	Type            DocType
	Status          DocStatus
	PublishedDate   time.Time
	CommentsEnabled bool
	Attachments     []*Attachment
}

type Attachment struct {
	Parent *Doc
	Url    string
}

var docType = map[string]DocType{
	"page": DocPage,
	"post": DocPost,
}

func buildDocFor(item *wxr.Item) *Doc {
	typ, ok := docType[item.PostType]
	if !ok {
		return nil
	}

	if item.PostParent != 0 {
		log.Fatalf("%q (%d): Docs with parents not yet supported.\n", item.Title, item.PostId)
	}

	name := item.PostName
	if name == "" {
		name = generatePostId(item.Title)
	}

	fmt.Printf("doc: %s\n", item.Title)
	urlRewrite := func(url string) string {
		//fmt.Printf("  %s\n", url)
		return url
	}

	markdown, err := ConvertHtmlToMarkdown(item.Content, urlRewrite)
	if err != nil {
		log.Fatalf("%q (%d): Error converting contents to markdown: %s\n", item.Title, item.PostId, err.Error())
	}

	return &Doc{
		Id:              name,
		Title:           item.Title,
		Content:         markdown,
		Type:            typ,
		Status:          parseDocStatus(item.Status),
		PublishedDate:   parseWpTime(item.PostDateGmt),
		CommentsEnabled: parseCommentsEnabled(item.CommentStatus),
	}
}

func convert(channel *wxr.Channel) *Blog {
	if len(channel.Authors) > 1 {
		log.Fatalf("Only one author supported right now.\n")
	}
	author := channel.Authors[0]

	blog := &Blog{
		Author: Author{
			Name:  author.DisplayName,
			Email: author.Email,
		},
	}

	// First pass: handle regular docs
	idsTaken := make(map[string]*Doc)
	docsByWpId := make(map[int]*Doc)
	for _, item := range channel.Items {
		if doc := buildDocFor(item); doc != nil {
			// NOTE: We can resolve ID collisions by just reassigning them to *make*
			// them unique, however right now, just don't handle that case.
			if other := idsTaken[doc.Id]; other != nil {
				log.Fatalf("Post name %q occurs twice (posts %q and %q).\n", doc.Id, other.Title, doc.Title)
			}
			idsTaken[doc.Id] = doc
			docsByWpId[item.PostId] = doc
			blog.Docs = append(blog.Docs, doc)
		}
	}

	// Second pass: handle attachments
	for _, item := range channel.Items {
		if item.PostType == "attachment" {
			parentDoc, ok := docsByWpId[item.PostParent]
			if !ok {
				log.Fatalf("Attachment %d refers to unknown parent %d.\n", item.PostId, item.PostParent)
			}
			parentDoc.Attachments = append(parentDoc.Attachments, &Attachment{
				Parent: parentDoc,
				Url:    item.AttachmentUrl,
			})
		}
	}

	return blog
}

func generatePostId(title string) string {
	// Cheesy way to generate post IDs
	// Restrict to ASCII lowercase characters and digits here
	return strings.Map(func(ch rune) rune {
		switch {
		case '0' <= ch && ch <= '9', 'a' <= ch && ch <= 'z', ch == '-', ch == '_':
			return ch
		case 'A' <= ch && ch <= 'Z':
			return ch - 'A' + 'a'
		case ch == ' ':
			return '-'
		}
		return -1
	}, title)
}

var commentType = map[string]CommentType{
	"":         CommentRegular,
	"pingback": CommentPingback,
}

func parseCommentType(typ string) CommentType {
	val, ok := commentType[typ]
	if !ok {
		log.Fatalf("unknown comment type %q\n", typ)
	}
	return val
}

var docStatus = map[string]DocStatus{
	"publish": StatusPublish,
	"draft":   StatusDraft,
	"pending": StatusPending,
	"private": StatusPrivate,
}

func parseDocStatus(status string) DocStatus {
	val, ok := docStatus[status]
	if !ok {
		log.Fatalf("unknown post status %q\n", status)
	}
	return val
}

var commentsEnabled = map[string]bool{
	"open":   true,
	"closed": false,
}

func parseCommentsEnabled(commentStatus string) bool {
	val, ok := commentsEnabled[commentStatus]
	if !ok {
		log.Fatalf("unknown comment status %q\n", commentStatus)
	}
	return val
}

func parseWpTime(val string) time.Time {
	if val == "0000-00-00 00:00:00" {
		return time.Time{}
	}

	time, err := time.Parse("2006-01-02 15:04:05", val)
	if err != nil {
		log.Fatalf("error parsing wordpress time %q: %s\n", val, err.Error())
	}
	return time
}

func readWxr(filename string) (*wxr.Rss, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	r := new(wxr.Rss)
	err = xml.Unmarshal(data, r)
	return r, err
}

func printComments(comments []*wxr.Comment) {
	for _, com := range comments {
		typ := parseCommentType(com.Type)
		if typ == CommentRegular {
			//fmt.Printf("  * %d by %s\n", com.Id, com.Author)
		}
	}
}

func writePost(wr io.Writer, doc *Doc) error {
	// write headers
	fmt.Fprintf(wr, "-title=%s\n", doc.Title)
	fmt.Fprintf(wr, "-time=%s\n", doc.PublishedDate.Format("2006-01-02 15:04:05"))
	if doc.Type == DocPage {
		fmt.Fprintf(wr, "-type=page\n")
	}

	// write content
	_, err := wr.Write(doc.Content)
	return err
}

func save(blog *Blog, dest string) error {
	if err := os.MkdirAll(dest, 0733); err != nil {
		return err
	}

	for _, doc := range blog.Docs {
		if doc.Status != StatusPublish {
			continue
		}

		fname := filepath.Join(dest, doc.Id+".md")
		if file, err := os.Create(fname); err == nil {
			err = writePost(file, doc)
			file.Close()
		} else {
			return err
		}
	}

	return nil
}

func main() {
	r, err := readWxr("c:\\Store\\Downloads\\therygblog.wordpress.2013-07-15.xml")
	if err != nil {
		fmt.Printf("Error reading WXR: %s\n", err.Error())
		return
	}

	blog := convert(&r.Channel)
	err = save(blog, "c:\\Store\\Blog\\posts")
	if err != nil {
		fmt.Printf("Error writing output: %s\n", err.Error())
	}

	fmt.Println("done.")
}
