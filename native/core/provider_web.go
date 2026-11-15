package core

import (
	"context"
	"errors"
	"fmt"
	htmlstd "html"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

type providerTextUserAgentKey struct{}
type providerTextNoCacheKey struct{}
type providerTextCookieKey struct{}

var webProviderNumericID = regexp.MustCompile(`^[1-9][0-9]{0,17}$`)

func webProviderInteger(value any, maximum int) (int, bool) {
	number, err := strconv.Atoi(nativeText(value))
	return number, err == nil && number >= 0 && number <= maximum
}

func providerHTMLAttr(node *html.Node, name string) string {
	if node != nil {
		for _, attribute := range node.Attr {
			if attribute.Key == name {
				return attribute.Val
			}
		}
	}
	return ""
}

func providerHTMLClass(node *html.Node, name string) bool {
	for _, value := range strings.Fields(providerHTMLAttr(node, "class")) {
		if value == name {
			return true
		}
	}
	return false
}

func providerHTMLNodes(root *html.Node, match func(*html.Node) bool) []*html.Node {
	var found []*html.Node
	if root == nil {
		return found
	}
	pending := []*html.Node{root}
	for len(pending) > 0 {
		node := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if match(node) {
			found = append(found, node)
		}
		for child := node.LastChild; child != nil; child = child.PrevSibling {
			pending = append(pending, child)
		}
	}
	return found
}

func providerHTMLFirstClass(root *html.Node, names ...string) *html.Node {
	for _, name := range names {
		matches := providerHTMLNodes(root, func(node *html.Node) bool {
			return providerHTMLClass(node, name)
		})
		if len(matches) > 0 {
			return matches[0]
		}
	}
	return nil
}

func providerHTMLText(root *html.Node) string {
	var text strings.Builder
	for _, node := range providerHTMLNodes(root, func(node *html.Node) bool {
		return node.Type == html.TextNode
	}) {
		if node.Parent != nil && (node.Parent.Data == "script" || node.Parent.Data == "style") {
			continue
		}
		text.WriteString(node.Data)
		text.WriteByte(' ')
	}
	return strings.Join(strings.Fields(text.String()), " ")
}

// providerHTMLPlainText converts provider synopsis fields to readable text.
// Providers commonly return either HTML fragments or HTML-escaped fragments;
// decoding once before parsing handles both without exposing markup tags.
func providerHTMLPlainText(raw string) string {
	raw = strings.TrimSpace(htmlstd.UnescapeString(raw))
	if raw == "" {
		return ""
	}
	document, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return strings.Join(strings.Fields(raw), " ")
	}
	return strings.TrimSpace(providerHTMLText(document))
}

func (d *Downloader) fetchProviderPage(ctx context.Context, address, referer, agent string) (*html.Node, string, error) {
	responses := &playbackResponseURLs{}
	ctx = context.WithValue(ctx, playbackResponseURLsKey{}, responses)
	if agent != "" {
		ctx = context.WithValue(ctx, providerTextUserAgentKey{}, agent)
	}
	body, err := d.fetchProviderText(ctx, address, referer)
	if err != nil {
		return nil, "", err
	}
	if len(body) > 4<<20 {
		return nil, "", errors.New("片源頁面過大")
	}
	if actual, found := responses.values.Load(address); found {
		address = actual.(string)
	}
	document, err := html.Parse(strings.NewReader(body))
	return document, address, err
}

func (d *Downloader) prepareWebProviderMedia(ctx context.Context, media providerMedia, sourceName string) (providerMedia, error) {
	address, err := url.Parse(media.URL)
	if err != nil || !isProviderHTTPMediaURL(media.URL) || address.User != nil {
		return providerMedia{}, fmt.Errorf("%s未提供有效的播放網址，請重新整理詳情後重試", sourceName)
	}
	if strings.HasSuffix(strings.ToLower(address.Path), ".m3u8") {
		if media.credentials != nil {
			ctx = providerMediaContext(ctx, media.credentials)
		}
		media.Playlist, media.URL, err = d.fetchMediaPlaylist(ctx, media.URL, media.Referer)
		if err != nil {
			return providerMedia{}, fmt.Errorf("取得%s播放清單失敗：%w", sourceName, err)
		}
		if !strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(media.Playlist, "\ufeff")), "#EXTM3U") {
			return providerMedia{}, fmt.Errorf("%s播放清單無效，請重新解析播放", sourceName)
		}
		if duration := m3u8Duration(media.Playlist); duration > 0 {
			media.Duration = duration
		}
	}
	return media, nil
}
