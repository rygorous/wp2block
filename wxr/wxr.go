// Definitions for Wordpress eXtended RSS (WXR)
package wxr

type Rss struct {
	Channel Channel `xml:"channel"`
}

type Channel struct {
	Title       string      `xml:"title"`
	Link        string      `xml:"link"`
	BaseBlogUrl string      `xml:"http://wordpress.org/export/1.2/ base_blog_url"`
	Authors     []*Author   `xml:"http://wordpress.org/export/1.2/ wp_author"`
	Categories  []*Category `xml:"http://wordpress.org/export/1.2/ category"`
	Items       []*Item     `xml:"item"`
}

type Author struct {
	Login       string `xml:"author_login"`
	Email       string `xml:"author_email"`
	DisplayName string `xml:"author_display_name"`
	FirstName   string `xml:"author_first_name"`
	LastName    string `xml:"author_last_name"`
}

type Category struct {
	TermId   int    `xml:"term_id"`
	NiceName string `xml:"category_nicename"`
	Parent   string `xml:"category_parent"`
	Name     string `xml:"cat_name"`
}

type Item struct {
	Title         string     `xml:"title"`
	Link          string     `xml:"link"`
	Content       []byte     `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
	PostId        int        `xml:"http://wordpress.org/export/1.2/ post_id"`
	PostDateGmt   string     `xml:"http://wordpress.org/export/1.2/ post_date_gmt"`
	PostName      string     `xml:"http://wordpress.org/export/1.2/ post_name"`
	PostType      string     `xml:"http://wordpress.org/export/1.2/ post_type"`
	PostParent    int        `xml:"http://wordpress.org/export/1.2/ post_parent"`
	CommentStatus string     `xml:"http://wordpress.org/export/1.2/ comment_status"`
	Status        string     `xml:"http://wordpress.org/export/1.2/ status"`
	IsSticky      int        `xml:"http://wordpress.org/export/1.2/ is_sticky"`
	Comments      []*Comment `xml:"http://wordpress.org/export/1.2/ comment"`
	Categories    []string   `xml:"category"`
	AttachmentUrl string     `xml:"http://wordpress.org/export/1.2/ attachment_url"`
}

type Comment struct {
	Id        int    `xml:"comment_id"`
	Author    string `xml:"comment_author"`
	AuthorUrl string `xml:"comment_author_url"`
	AuthorIp  string `xml:"comment_author_ip"`
	DateGmt   string `xml:"comment_date_gmt"`
	Content   string `xml:"comment_content"`
	Approved  string `xml:"comment_approved"`
	Type      string `xml:"comment_type"`
	Parent    int    `xml:"comment_parent"`
	UserId    int    `xml:"comment_user_id"`
}
