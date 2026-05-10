package web

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/colormechadd/mailaroo/internal/proxy"
)

var privateIPNets = func() []*net.IPNet {
	var nets []*net.IPNet
	for _, cidr := range []string{
		"127.0.0.0/8",
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"169.254.0.0/16",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
	} {
		_, ipNet, _ := net.ParseCIDR(cidr)
		nets = append(nets, ipNet)
	}
	return nets
}()

func isPrivateIP(ip net.IP) bool {
	for _, ipNet := range privateIPNets {
		if ipNet.Contains(ip) {
			return true
		}
	}
	return false
}

// ssrfSafeDialContext resolves the target hostname and rejects connections to
// private/loopback IP ranges before dialing. This check runs after DNS
// resolution so it catches DNS-based SSRF attempts.
func ssrfSafeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, a := range addrs {
		ip := net.ParseIP(a)
		if ip == nil || isPrivateIP(ip) {
			return nil, fmt.Errorf("proxy: address %s is not allowed", a)
		}
	}
	return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(addrs[0], port))
}

var proxyHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		DialContext: ssrfSafeDialContext,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("proxy: too many redirects")
		}
		return nil
	},
}

// handleProxyImage fetches an externally-hosted image server-side and streams
// it to the client, preventing the sender from learning the reader's IP.
// The URL is HMAC-signed to prevent open-redirect and SSRF abuse.
func (s *Server) handleProxyImage(w http.ResponseWriter, r *http.Request) {
	rawURLBytes, err := base64.RawURLEncoding.DecodeString(r.URL.Query().Get("url"))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	rawURL := string(rawURLBytes)
	sig := r.URL.Query().Get("sig")

	if !proxy.VerifyURL(s.proxyKey, rawURL, sig) {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Maileroo-ImageProxy/1.0)")
	req.Header.Set("Via", "1.1 mailaroo-image-proxy")
	if v := r.Header.Get("If-None-Match"); v != "" {
		req.Header.Set("If-None-Match", v)
	}
	if v := r.Header.Get("If-Modified-Since"); v != "" {
		req.Header.Set("If-Modified-Since", v)
	}
	resp, err := proxyHTTPClient.Do(req)
	if err != nil {
		slog.Error("image proxy fetch failed", "url", rawURL, "err", err)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	ct := resp.Header.Get("Content-Type")
	mediaType, _, _ := mime.ParseMediaType(ct)
	if !strings.HasPrefix(mediaType, "image/") {
		slog.Error("image proxy received non-image content type", "url", rawURL, "content_type", ct)
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}

	w.Header().Set("Content-Type", ct)
	for _, h := range []string{"Cache-Control", "ETag", "Last-Modified", "Content-Length", "Expires"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	io.Copy(w, io.LimitReader(resp.Body, 10<<20))
}
