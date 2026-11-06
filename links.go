package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

//go:embed resources/link-globe.png
var linkGlobe []byte

// Only the scheme and DNS spelling are normalized. Escaped path, query order,
// explicit ports, trailing slash and fragment retain their submitted meaning.
func normalizeLink(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("Enter a URL.")
	}
	if len(raw) > 8192 {
		return "", fmt.Errorf("Use a URL shorter than 8,192 bytes.")
	}

	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	} else {
		colon, boundary := strings.IndexByte(raw, ':'), strings.IndexAny(raw, "/?#")
		explicitHTTP := false
		if colon >= 0 && (boundary < 0 || colon < boundary) && !strings.HasPrefix(raw, "[") {
			scheme := strings.ToLower(raw[:colon])
			switch scheme {
			case "javascript", "data", "file", "ftp", "mailto", "about", "tel", "sms", "ssh", "ws", "wss", "blob":
				return "", fmt.Errorf("Use an HTTP or HTTPS URL.")
			}
			explicitHTTP = (scheme == "http" || scheme == "https") && strings.HasPrefix(raw[colon+1:], "//")
			if !explicitHTTP {
				port := raw[colon+1:]
				if end := strings.IndexAny(port, "/?#"); end >= 0 {
					port = port[:end]
				}
				digits := port != "" && strings.IndexFunc(port, func(r rune) bool { return r < '0' || r > '9' }) < 0
				if !digits {
					return "", fmt.Errorf("Use an HTTP or HTTPS URL.")
				}
			}
		}
		if !explicitHTTP {
			raw = "https://" + raw
		}
	}

	u, err := url.Parse(raw)
	if err != nil || !validHTTPURL(u) {
		return "", fmt.Errorf("Enter an HTTP or HTTPS URL with a valid host.")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	normalized := u.String()
	if len(normalized) > 8192 {
		return "", fmt.Errorf("Use a URL shorter than 8,192 bytes after escaping.")
	}
	return normalized, nil
}

func validHTTPURL(u *url.URL) bool {
	if u == nil || (strings.ToLower(u.Scheme) != "http" && strings.ToLower(u.Scheme) != "https") || u.Opaque != "" || u.User != nil || u.Hostname() == "" {
		return false
	}
	host := u.Hostname()
	if strings.HasSuffix(u.Host, ":") {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	return !strings.ContainsAny(host, " \\%") && !strings.ContainsFunc(host, unicode.IsControl) && u.Port() != "0"
}

func linkDigest(raw string) string {
	h := sha256.Sum256([]byte("link\x00" + raw))
	return hex.EncodeToString(h[:])
}
func validLinkCapture(c capture) bool {
	normalized, err := normalizeLink(c.URL)
	return err == nil && normalized == c.URL && c.Title != "" && len(c.Title) <= 4096 && c.SHA256 == linkDigest(c.URL) &&
		c.MediaType == "link" && c.Format == "webloc" && c.Width > 0 && c.Height > 0 && c.Width <= 16384 && c.Height <= 16384
}

type linkMetadata struct {
	Title                  string
	Images, Twitter, Icons []string
}

func cleanLinkTitle(s string) string {
	s = strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
	r := []rune(s)
	if len(r) > 1024 {
		s = string(r[:1024])
	}
	return s
}
func parseLinkMetadata(data []byte, contentType string) (linkMetadata, error) {
	var m linkMetadata
	reader, err := charset.NewReader(bytes.NewReader(data), contentType)
	if err != nil {
		return m, err
	}
	doc, err := html.Parse(reader)
	if err != nil {
		return m, err
	}
	var ogTitle, title string
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode {
			attrs := map[string]string{}
			for _, a := range n.Attr {
				attrs[strings.ToLower(a.Key)] = a.Val
			}
			if n.Data == "meta" {
				key := strings.ToLower(attrs["property"])
				if key == "" {
					key = strings.ToLower(attrs["name"])
				}
				switch key {
				case "og:title":
					if ogTitle == "" {
						ogTitle = cleanLinkTitle(attrs["content"])
					}
				case "og:image", "og:image:url", "og:image:secure_url":
					if len(m.Images) < 8 {
						m.Images = append(m.Images, attrs["content"])
					}
				case "twitter:image", "twitter:image:src":
					if len(m.Twitter) < 4 {
						m.Twitter = append(m.Twitter, attrs["content"])
					}
				}
			}
			if n.Data == "title" && title == "" {
				var b strings.Builder
				var text func(*html.Node)
				text = func(x *html.Node) {
					if x.Type == html.TextNode {
						b.WriteString(x.Data)
					}
					for c := x.FirstChild; c != nil; c = c.NextSibling {
						text(c)
					}
				}
				text(n)
				title = cleanLinkTitle(b.String())
			}
			if n.Data == "link" && len(m.Icons) < 8 {
				for _, rel := range strings.Fields(strings.ToLower(attrs["rel"])) {
					if rel == "icon" || rel == "apple-touch-icon" {
						m.Icons = append(m.Icons, attrs["href"])
						break
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(doc)
	m.Title = ogTitle
	if m.Title == "" {
		m.Title = title
	}
	return m, nil
}

func linkHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxResponseHeaderBytes = 64 << 10
	transport.ResponseHeaderTimeout = 4 * time.Second
	return &http.Client{Transport: transport, Timeout: 6 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !validHTTPURL(req.URL) {
			return fmt.Errorf("unsupported redirect or too many redirects")
		}
		return nil
	}}
}
func fetchLinkBytes(ctx context.Context, client *http.Client, u *url.URL, limit int64) ([]byte, *url.URL, string, error) {
	if !validHTTPURL(u) {
		return nil, nil, "", fmt.Errorf("unsupported URL")
	}
	requestURL := *u
	requestURL.Fragment = ""
	requestURL.RawFragment = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, nil, "", err
	}
	req.Header.Set("User-Agent", "Keeps/0.1 (bookmark metadata)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > limit {
		return nil, nil, "", fmt.Errorf("response too large")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if int64(len(data)) > limit {
		return nil, nil, "", fmt.Errorf("response too large")
	}
	return data, resp.Request.URL, resp.Header.Get("Content-Type"), err
}

type preparedLink struct {
	item  capture
	stage string
}

func (p *preparedLink) cleanup() {
	_ = os.RemoveAll(p.stage)
}

// All HTTP, parsing, image normalization and composition finish before enqueue.
func (s *libraryStore) prepareLink(ctx context.Context, raw string) (_ *preparedLink, err error) {
	normalized, err := normalizeLink(raw)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(s.root, 0755); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(s.root, ".incoming-link-")
	if err != nil {
		return nil, err
	}
	p := &preparedLink{stage: stage}
	success := false
	defer func() {
		if !success {
			p.cleanup()
		}
	}()
	id, err := newLibraryID()
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(normalized)
	p.item = capture{ID: id, Kind: captureLink, Filename: u.Hostname() + ".webloc", URL: normalized, Title: u.Hostname(),
		MediaType: "link", Format: "webloc", Width: 3, Height: 2, SHA256: linkDigest(normalized)}
	p.item.setManagedPaths()
	client := linkHTTPClient()
	defer client.CloseIdleConnections()
	networkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	metadata := linkMetadata{}
	base := u
	if data, final, ct, fetchErr := fetchLinkBytes(networkCtx, client, u, 2<<20); fetchErr == nil {
		base = final
		metadata, _ = parseLinkMetadata(data, ct)
	}
	if metadata.Title != "" {
		p.item.Title = metadata.Title
	}
	preview := filepath.Join(stage, stagedPreview)
	favicon := filepath.Join(stage, stagedFavicon)
	asset := func(candidates []string, output string, edge int) (mediaInfo, bool) {
		for _, candidate := range candidates {
			if networkCtx.Err() != nil {
				break
			}
			relative, e := url.Parse(strings.TrimSpace(candidate))
			if e != nil || candidate == "" {
				continue
			}
			resolved := base.ResolveReference(relative)
			data, _, _, e := fetchLinkBytes(networkCtx, client, resolved, 8<<20)
			if e != nil {
				continue
			}
			input := filepath.Join(stage, "download")
			if e = os.WriteFile(input, data, 0600); e != nil {
				continue
			}
			info, e := nativeLinkImage(networkCtx, input, output, edge)
			_ = os.Remove(input)
			if e == nil {
				return info, true
			}
		}
		return mediaInfo{}, false
	}
	info, hasPreview := asset(append(slices.Clone(metadata.Images), metadata.Twitter...), preview, 1024)
	if hasPreview {
		p.item.Width, p.item.Height = info.Width, info.Height
	} else {
		if err = nativeLinkPlaceholder(ctx, preview); err != nil {
			return nil, err
		}
	}
	origin := *base
	origin.Path = "/favicon.ico"
	origin.RawPath = ""
	origin.RawQuery = ""
	origin.ForceQuery = false
	origin.Fragment = ""
	origin.RawFragment = ""
	_, hasIcon := asset(append(slices.Clone(metadata.Icons), origin.String()), favicon, 64)
	if !hasIcon {
		if err = os.WriteFile(favicon, linkGlobe, 0600); err != nil {
			return nil, err
		}
	}
	var escaped bytes.Buffer
	_ = xml.EscapeText(&escaped, []byte(normalized))
	bookmark := []byte("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\"><dict><key>URL</key><string>" + escaped.String() + "</string></dict></plist>\n")
	if err = os.WriteFile(filepath.Join(stage, stagedOriginal), bookmark, 0600); err != nil {
		return nil, err
	}
	composition := linkComposition{Image: preview, Favicon: favicon, Title: p.item.Title, URL: normalized}
	if err = nativeLinkCompose(ctx, filepath.Join(stage, "composition.json"), filepath.Join(stage, stagedQuickLook), composition); err != nil {
		return nil, err
	}
	for _, asset := range p.item.assets() {
		if err = syncFile(filepath.Join(stage, asset.staged)); err != nil {
			return nil, err
		}
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	success = true
	return p, nil
}

func (s *libraryStore) saveLink(p *preparedLink, destination string) (result importResult) {
	fail := func(err error) importResult { result.Fatal = err; return result }
	unlock, err := s.lockMetadata()
	if err != nil {
		return fail(err)
	}
	defer unlock()
	m, err := s.readManifest()
	if err != nil {
		return fail(err)
	}
	if destination != "" && !hasCollection(m.Collections, destination) {
		destination = ""
		result.DestinationMissing = true
	}
	result.Snapshot = m
	for _, old := range m.PendingPurges {
		if old.isLink() && old.URL == p.item.URL {
			return fail(fmt.Errorf("This link is awaiting Trash cleanup. Try again after cleanup finishes."))
		}
	}
	for i, old := range m.Items {
		if old.isLink() && old.URL == p.item.URL {
			if old.TrashedAt != nil {
				result.InTrash = 1
			} else if destination != "" && !slices.Contains(old.CollectionIDs, destination) {
				old.CollectionIDs = append(slices.Clone(old.CollectionIDs), destination)
				m.Items[i] = old
				if err = s.writeManifest(m); err != nil {
					return fail(err)
				}
			}
			result.Duplicates = 1
			result.Snapshot = m
			return result
		}
	}
	if err = s.createManagedDirs(); err != nil {
		return fail(err)
	}
	item := p.item
	item.ImportedAt = time.Now().UTC()
	if destination != "" {
		item.CollectionIDs = []string{destination}
	}
	if err = s.installCapture(&m, item, p.stage); err != nil {
		return fail(err)
	}
	result.Snapshot, result.Added = m, 1
	return result
}
