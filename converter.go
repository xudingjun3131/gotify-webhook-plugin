package main

import (
	"regexp"
	"strings"

	htmltomd "github.com/JohannesKaufmann/html-to-markdown/v2"
	"golang.org/x/net/html"
)

// ConvertHTMLToMarkdown converts an HTML string to Markdown using the html-to-markdown library.
// It preserves literal \n characters by converting them to <br> before processing.
func ConvertHTMLToMarkdown(htmlContent string) (string, error) {
	if strings.TrimSpace(htmlContent) == "" {
		return "", nil
	}

	// HTML 会将 \n 视为空白折叠，先将 \n 转为 <br> 以保留换行
	htmlContent = strings.ReplaceAll(htmlContent, "\n", "<br>")

	md, err := htmltomd.ConvertString(htmlContent)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(md), nil
}

// htmlTagPattern matches common HTML tags like <div>, <p>, <br/>, <h1>, etc.
var htmlTagPattern = regexp.MustCompile(`<\s*(html|head|body|div|span|p|br|h[1-6]|ul|ol|li|table|tr|td|th|a|img|b|i|em|strong|code|pre|blockquote|hr|form|input|label|select|option|textarea|button|style|script|link|meta|title|header|footer|nav|section|article|main|aside|figure|figcaption|details|summary|mark|sub|sup|del|ins|small|big|center|font|iframe|video|audio|source|canvas|svg|dl|dt|dd|fieldset|legend|caption|thead|tbody|tfoot|col|colgroup|abbr|address|cite|dfn|kbd|samp|var|wbr|noscript)\b[^>]*>`)

// IsHTML checks if a string likely contains HTML content.
func IsHTML(s string) bool {
	return htmlTagPattern.MatchString(s)
}

// ExtractHTMLTitle tries to extract a title from the HTML content.
// It looks for <title> first, then falls back to the first <h1>.
func ExtractHTMLTitle(htmlContent string) string {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return ""
	}

	// Try <title> first, then <h1>
	for _, tag := range []string{"title", "h1"} {
		if text := findFirstElementText(doc, tag); text != "" {
			return text
		}
	}
	return ""
}

// findFirstElementText finds the first occurrence of a given tag and returns its text content.
func findFirstElementText(n *html.Node, tag string) string {
	if n.Type == html.ElementNode && n.Data == tag {
		return collectText(n)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if result := findFirstElementText(c, tag); result != "" {
			return result
		}
	}
	return ""
}

// collectText collects all text content from a node and its children.
func collectText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var sb strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		sb.WriteString(collectText(c))
	}
	return strings.TrimSpace(sb.String())
}
