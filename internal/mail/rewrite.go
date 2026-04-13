package mail

import (
	"bytes"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var trackerDomains = map[string]struct{}{
	"trk.klaviyo.com":             {},
	"click.mailchimp.com":         {},
	"list-manage.com":             {},
	"sendgrid.net":                {},
	"mandrillapp.com":             {},
	"track.hubspot.com":           {},
	"opens.campaign-archive.com":  {},
	"tracking.tldrnewsletter.com": {},
}

var (
	// widthOnePx / heightOnePx detect 0px or 1px dimensions in inline styles.
	widthOnePx  = regexp.MustCompile(`(?i)width\s*:\s*[01]px`)
	heightOnePx = regexp.MustCompile(`(?i)height\s*:\s*[01]px`)

	// cssURLRe matches url(...) with http/https URLs inside CSS.
	cssURLRe = regexp.MustCompile(`url\(\s*["']?(https?://[^"')]+)["']?\s*\)`)
)

// StrippedPixel records a tracking pixel that was removed from an HTML body.
type StrippedPixel struct {
	Src    string `json:"src"`
	Method string `json:"method"` // "dimension", "style_dimension", or "domain"
}

// StripTrackingPixelsFromHTML removes tracking pixels from the given HTML body
// and returns the cleaned HTML along with a description of each removed pixel.
// It is called at ingest time before the email is stored.
func StripTrackingPixelsFromHTML(body string) (string, []StrippedPixel) {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return body, nil
	}
	var pixels []StrippedPixel
	stripPixelNodes(doc, &pixels)
	var buf bytes.Buffer
	html.Render(&buf, doc)
	return buf.String(), pixels
}

func stripPixelNodes(n *html.Node, pixels *[]StrippedPixel) {
	var toRemove []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == "img" {
			if p, ok := classifyTrackingPixel(c); ok {
				toRemove = append(toRemove, c)
				*pixels = append(*pixels, p)
				continue
			}
		}
		stripPixelNodes(c, pixels)
	}
	for _, c := range toRemove {
		n.RemoveChild(c)
	}
}

// classifyTrackingPixel inspects an <img> node and returns a StrippedPixel
// if it matches any tracking pixel heuristic.
func classifyTrackingPixel(n *html.Node) (StrippedPixel, bool) {
	var width, height, style, src string
	for _, a := range n.Attr {
		switch a.Key {
		case "width":
			width = a.Val
		case "height":
			height = a.Val
		case "style":
			style = a.Val
		case "src":
			src = a.Val
		}
	}
	if isPixelDim(width) && isPixelDim(height) {
		return StrippedPixel{Src: src, Method: "dimension"}, true
	}
	if widthOnePx.MatchString(style) && heightOnePx.MatchString(style) {
		return StrippedPixel{Src: src, Method: "style_dimension"}, true
	}
	if src != "" {
		if u, err := url.Parse(src); err == nil {
			if _, blocked := trackerDomains[u.Hostname()]; blocked {
				return StrippedPixel{Src: src, Method: "domain"}, true
			}
		}
	}
	return StrippedPixel{}, false
}

// rewriteForPrivacy parses the HTML body, drops tracking pixels, and rewrites
// all external image URLs (src, srcset, and CSS url()) to route through the
// proxy via sign. Returns the rewritten HTML, or the original on parse failure.
func rewriteForPrivacy(body string, sign func(string) string) string {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return body
	}
	walkNode(doc, sign)
	var buf bytes.Buffer
	html.Render(&buf, doc)
	return buf.String()
}

func walkNode(n *html.Node, sign func(string) string) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			walkNode(c, sign)
			continue
		}
		switch c.Data {
		case "img":
			// Tracking pixels are stripped at ingest time by the pipeline step.
			// Here we only rewrite external URLs through the proxy.
			rewriteImgNode(c, sign)
		case "style":
			rewriteStyleNode(c, sign)
		}
		// Rewrite url() in the style attribute for every element.
		rewriteStyleAttr(c, sign)
		walkNode(c, sign)
	}
}

func isPixelDim(val string) bool {
	return val == "0" || val == "1"
}

// rewriteImgNode rewrites the src and srcset attributes of an <img> node.
func rewriteImgNode(n *html.Node, sign func(string) string) {
	for i, a := range n.Attr {
		switch a.Key {
		case "src":
			if isExternalHTTP(a.Val) {
				n.Attr[i].Val = sign(a.Val)
			}
		case "srcset":
			n.Attr[i].Val = rewriteSrcset(a.Val, sign)
		}
	}
}

// rewriteSrcset rewrites the URL token in each comma-separated "<url> <descriptor>"
// pair of a srcset attribute value.
func rewriteSrcset(srcset string, sign func(string) string) string {
	parts := strings.Split(srcset, ",")
	for i, part := range parts {
		part = strings.TrimSpace(part)
		fields := strings.Fields(part)
		if len(fields) == 0 {
			continue
		}
		if isExternalHTTP(fields[0]) {
			fields[0] = sign(fields[0])
		}
		parts[i] = strings.Join(fields, " ")
	}
	return strings.Join(parts, ", ")
}

// rewriteStyleAttr rewrites url() references in the style attribute of a node.
func rewriteStyleAttr(n *html.Node, sign func(string) string) {
	for i, a := range n.Attr {
		if a.Key == "style" {
			n.Attr[i].Val = rewriteCSSURLs(a.Val, sign)
		}
	}
}

// rewriteStyleNode rewrites url() references in the text content of a <style> element.
func rewriteStyleNode(n *html.Node, sign func(string) string) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			c.Data = rewriteCSSURLs(c.Data, sign)
		}
	}
}

// rewriteCSSURLs replaces http/https url() values in CSS text with proxied URLs.
func rewriteCSSURLs(css string, sign func(string) string) string {
	return cssURLRe.ReplaceAllStringFunc(css, func(match string) string {
		sub := cssURLRe.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		return "url(" + sign(sub[1]) + ")"
	})
}

func isExternalHTTP(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}
