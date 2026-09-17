package e2e

import (
	"errors"
	stdhtml "html"
	"io"
	"net/http"
	"strings"

	"golang.org/x/net/html"
)

type Item struct{ Name, URL string }

// Fetch deliberately drops transport errors and response headers: they can
// contain cookies, authorization codes, IdP redirects, and reflected input.
func Fetch(client *http.Client, origin, path, cookie string) (int, string, error) {
	req, err := http.NewRequest(http.MethodGet, origin+path, nil)
	if err != nil {
		return 0, "", errors.New("HTTP request invalid")
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "__Host-portal_session", Value: cookie})
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", errors.New("HTTP request failed")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20+1))
	if err != nil || len(body) > 2<<20 {
		return 0, "", errors.New("HTTP response unreadable or oversized")
	}
	return resp.StatusCode, string(body), nil
}

func CheckCatalog(client *http.Client, origin, cookie string, visible, hidden []Item, stale bool) error {
	status, body, err := Fetch(client, origin, "/", cookie)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return errors.New("catalog did not return HTTP 200")
	}
	for _, item := range hidden {
		if strings.Contains(body, stdhtml.EscapeString(item.Name)) || strings.Contains(body, item.URL) {
			return errors.New("unauthorized name or URL disclosed")
		}
	}
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return errors.New("catalog HTML invalid")
	}
	links := map[Item]bool{}
	var staleBanner bool
	var text func(*html.Node) string
	text = func(n *html.Node) string {
		if n.Type == html.TextNode {
			return n.Data
		}
		var s string
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			s += text(c)
		}
		return s
	}
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		for _, a := range n.Attr {
			if n.Data == "a" && a.Key == "href" {
				links[Item{Name: strings.TrimSpace(text(n)), URL: a.Val}] = true
			}
			if a.Key == "class" {
				for _, class := range strings.Fields(a.Val) {
					if class == "stale-banner" {
						staleBanner = true
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(doc)
	for _, item := range visible {
		if !links[item] {
			return errors.New("expected catalog name/current HTTPS link absent")
		}
	}
	if staleBanner != stale {
		return errors.New("catalog stale indication differs from expected state")
	}
	return nil
}
