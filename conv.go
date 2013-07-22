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
	"math/rand"
	"net/url"
	"os"
	"path"
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

	mediaPath = "wpmedia"
)

type Blog struct {
	Author      Author
	Docs        []*Doc
	Attachments []*Attachment
}

type Author struct {
	Name  string
	Email string
}

type Doc struct {
	Id              string
	Title           string
	Content         []byte // output markdown
	ContentHtml     []byte // original HTML code
	Type            DocType
	Status          DocStatus
	PublishedDate   time.Time
	CommentsEnabled bool
}

type Attachment struct {
	Parent   *Doc
	Url      string // Url on the Wordpress site
	Filename string // Local media file name
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

	return &Doc{
		Id:              name,
		Title:           item.Title,
		ContentHtml:     item.Content,
		Type:            typ,
		Status:          parseDocStatus(item.Status),
		PublishedDate:   parseWpTime(item.PostDateGmt),
		CommentsEnabled: parseCommentsEnabled(item.CommentStatus),
	}
}

type urlRewriter struct {
	docsByUrl    map[string]*Doc
	attsByUrl    map[string]*Attachment
	filenameUsed map[string]bool
}

func (u *urlRewriter) UrlRewrite(target string) string {
	if parsed, err := url.Parse(target); err == nil {
		canonical := url.URL{
			Scheme: parsed.Scheme,
			Host:   parsed.Host,
			Path:   parsed.Path,
		}
		canonicalUrl := canonical.String()
		// try to look up as a doc
		tgtDoc := u.docsByUrl[canonicalUrl]
		if tgtDoc == nil && !strings.HasSuffix(canonicalUrl, "/") {
			tgtDoc = u.docsByUrl[canonicalUrl+"/"]
		}
		if tgtDoc != nil {
			dest := "*" + tgtDoc.Id
			if parsed.Fragment != "" {
				dest += "#" + parsed.Fragment
			}
			//fmt.Printf("  -> %s\n", tgtDoc.Title)
			return dest
		}
		// try to look up as attachment
		tgtAtt := u.attsByUrl[canonicalUrl]
		if tgtAtt != nil {
			u.useAttachment(tgtAtt)
			return mediaPath + "/" + tgtAtt.Filename
		}
	}
	// debug only!
	if strings.HasPrefix(target, "http://fgiesen.") {
		fmt.Printf("  unresolved self-link %q\n", target)
	}
	return target
}

func (u *urlRewriter) useAttachment(a *Attachment) {
	// if we've already assigned a file name, we're good!
	if a.Filename != "" {
		return
	}

	parsed, err := url.Parse(a.Url)
	if err != nil {
		log.Fatalf("Can't parse attachment URL %q", a.Url)
	}

	// try original name
	basename := path.Base(parsed.Path)
	if u.tryAttachmentFilename(a, basename) {
		return
	}

	// if that didn't work, just keep trying random 32-bit prefixes to
	// make it unique
	for {
		name := fmt.Sprintf("%08x_%s", rand.Uint32(), basename)
		if u.tryAttachmentFilename(a, name) {
			break
		}
	}
}

func (u *urlRewriter) tryAttachmentFilename(a *Attachment, filename string) bool {
	namel := strings.ToLower(filename)
	if !u.filenameUsed[namel] {
		a.Filename = filename
		u.filenameUsed[namel] = true
		return true
	}
	return false
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
	var rewriter urlRewriter

	idsTaken := make(map[string]*Doc)
	docsByWpId := make(map[int]*Doc)
	rewriter.docsByUrl = make(map[string]*Doc)
	rewriter.attsByUrl = make(map[string]*Attachment)
	rewriter.filenameUsed = make(map[string]bool)
	for _, item := range channel.Items {
		if doc := buildDocFor(item); doc != nil {
			// NOTE: We can resolve ID collisions by just reassigning them to *make*
			// them unique, however right now, just don't handle that case.
			if other := idsTaken[doc.Id]; other != nil {
				log.Fatalf("Post name %q occurs twice (posts %q and %q).\n", doc.Id, other.Title, doc.Title)
			}
			idsTaken[doc.Id] = doc
			docsByWpId[item.PostId] = doc
			rewriter.docsByUrl[item.Link] = doc
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
			att := &Attachment{
				Parent: parentDoc,
				Url:    item.AttachmentUrl,
			}
			rewriter.attsByUrl[att.Url] = att
			blog.Attachments = append(blog.Attachments, att)
		}
	}

	// Generate markdown for docs
	for _, doc := range blog.Docs {
		//fmt.Printf("doc: %s\n", doc.Title)

		var err error
		doc.Content, err = ConvertHtmlToMarkdown(doc.ContentHtml, &rewriter)
		if err != nil {
			log.Fatalf("%q: Error converting contents to markdown: %s\n", doc.Title, err.Error())
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
	if err := os.MkdirAll(filepath.Join(dest, mediaPath), 0733); err != nil {
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
