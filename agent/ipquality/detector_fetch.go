package ipquality

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type detectorEndpoint struct {
	URL   string
	Hosts []string
	Limit int64
}

func detectorEndpointFor(id, target string) (detectorEndpoint, bool) {
	e := detectorEndpoint{Limit: serviceBodyLimit}
	switch id {
	case "netflix-81280792", "netflix-70143836", "netflix-80057281":
		e.URL = "https://www.netflix.com/title/" + strings.TrimPrefix(id, "netflix-")
		e.Hosts = []string{"www.netflix.com", "netflix.com"}
	case "youtube-premium":
		e.URL = "https://www.youtube.com/premium"
		e.Hosts = []string{"www.youtube.com", "consent.youtube.com", "consent.google.com"}
	case "amazon-prime-video":
		e.URL = "https://www.primevideo.com/"
		e.Hosts = []string{"www.primevideo.com", "primevideo.com"}
		e.Limit = 8 * 1024 * 1024
	case "openai-cookie":
		e.URL = "https://api.openai.com/compliance/cookie_requirements"
		e.Hosts = []string{"api.openai.com"}
	case "openai-ios":
		e.URL = "https://ios.chat.openai.com/"
		e.Hosts = []string{"ios.chat.openai.com"}
	case "geo-maxmind":
		e.URL = "https://ipinfo.check.place/" + url.PathEscape(target) + "?lang=en"
		e.Hosts = []string{"ipinfo.check.place"}
	case "geo-ipinfo":
		e.URL = "https://ipinfo.io/widget/demo/" + url.PathEscape(target)
		e.Hosts = []string{"ipinfo.io"}
	default:
		return e, false
	}
	return e, true
}

func validDetectorTarget(target string, family int) bool {
	ip, err := netip.ParseAddr(target)
	return err == nil && ip.String() == target && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.Is4In6() && (family == 4 && ip.Is4() || family == 6 && ip.Is6())
}

var detectorNonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"),
}

func publicDetectorDestination(ip netip.Addr, family int) bool {
	if !validDetectorTarget(ip.String(), family) {
		return false
	}
	for _, prefix := range detectorNonPublicPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

func allowedDetectorURL(e detectorEndpoint, u *url.URL) bool {
	if u == nil || u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	for _, host := range e.Hosts {
		if u.Hostname() == host {
			return true
		}
	}
	return false
}

// Resolve and dial the same validated address, avoiding a DNS rebinding window.
// A redirect cannot expand this policy, including to a private address on an allowed host.
func detectorHTTPClient(family int) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	dialer := &net.Dialer{}
	transport.DialContext = func(ctx context.Context, _ string, address string) (net.Conn, error) {
		return dialDetectorAddress(ctx, family, address, net.DefaultResolver.LookupNetIP, dialer.DialContext)
	}
	return &http.Client{Transport: transport, Timeout: 10 * time.Second}
}

func dialDetectorAddress(ctx context.Context, family int, address string, lookup func(context.Context, string, string) ([]netip.Addr, error), dial func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	network, tcp := "ip4", "tcp4"
	if family == 6 {
		network, tcp = "ip6", "tcp6"
	}
	ips, err := lookup(ctx, network, host)
	if err != nil {
		return nil, err
	}
	var last error
	for _, ip := range ips {
		if !publicDetectorDestination(ip, family) {
			last = errServiceRedirect
			continue
		}
		conn, err := dial(ctx, tcp, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	if last == nil {
		last = errors.New("no usable address")
	}
	return nil, last
}

type detectorFacts struct {
	HTTPStatus     int               `json:"http_status"`
	ErrorCode      string            `json:"error_code"`
	Challenge      bool              `json:"challenge"`
	HasHeading     bool              `json:"has_heading"`
	Login          bool              `json:"login"`
	Title          string            `json:"title"`
	Text           string            `json:"text"`
	Metas          map[string]string `json:"metas"`
	Values         map[string][]any  `json:"values"`
	YouTubeHeaders []any             `json:"youtube_headers"`
	YouTubeOffers  []any             `json:"youtube_offers"`
	JSON           map[string]any    `json:"json"`
}

// Only explicit Premium renderer subtrees enter the shell's business decision.
// Arbitrary script strings, comments and unrelated renderer nodes are not evidence.
func detectorYouTubeNodes(root any, headers, offers *[]any) {
	object, ok := root.(map[string]any)
	if !ok {
		return
	}
	contents, _ := object["contents"].(map[string]any)
	browse, _ := contents["twoColumnBrowseResultsRenderer"].(map[string]any)
	tabs, _ := browse["tabs"].([]any)
	for _, tab := range tabs {
		t, _ := tab.(map[string]any)
		renderer, _ := t["tabRenderer"].(map[string]any)
		content, _ := renderer["content"].(map[string]any)
		list, _ := content["sectionListRenderer"].(map[string]any)
		items, _ := list["contents"].([]any)
		for _, item := range items {
			i, _ := item.(map[string]any)
			section, _ := i["itemSectionRenderer"].(map[string]any)
			groups, _ := section["contents"].([]any)
			for _, group := range groups {
				g, _ := group.(map[string]any)
				model, _ := g["sectionGroupViewModel"].(map[string]any)
				sections, _ := model["sections"].([]any)
				for _, entry := range sections {
					node, _ := entry.(map[string]any)
					if v, ok := node["lpHeaderSectionViewModel"].(map[string]any); ok {
						*headers = append(*headers, v)
					}
					if v, ok := node["lpOfferCardSectionViewModel"].(map[string]any); ok {
						*offers = append(*offers, v)
					}
				}
			}
		}
	}
}

func extractDetectorFacts(id string, status int, final *url.URL, body []byte) detectorFacts {
	page := parseServicePage(body)
	facts := detectorFacts{HTTPStatus: status, Challenge: page.challenge, HasHeading: page.hasHeading, Login: page.login || loginServiceURL(final), Title: page.title, Text: page.text, Metas: page.metas, Values: make(map[string][]any)}
	if final != nil && (final.Hostname() == "consent.youtube.com" || final.Hostname() == "consent.google.com") {
		facts.Challenge = true
	}
	for _, key := range []string{"currentTerritory", "isServiceRestricted", "contentRegion", "INNERTUBE_CONTEXT_GL"} {
		objects := page.objects
		if id == "youtube-premium" {
			objects = page.youtubeObjects
		}
		facts.Values[key] = serviceValues(objects, key)
	}
	for _, object := range page.youtubeObjects {
		detectorYouTubeNodes(object, &facts.YouTubeHeaders, &facts.YouTubeOffers)
	}
	// Keep the paired country object, never a country code from an unrelated string.
	for _, object := range page.objects {
		visitServiceObjects(object, func(node map[string]any) {
			if name, ok := node["countryName"].(string); ok && name != "" {
				if code := serviceCountry(node["id"]); code != "" {
					facts.Values["netflixCountry"] = append(facts.Values["netflixCountry"], code)
				}
			}
		})
	}
	facts.JSON, _ = serviceJSON(body)
	return facts
}

func fetchDetectorEndpoint(ctx context.Context, client *http.Client, id, target string, family int, dir string) (detectorFacts, error) {
	facts := detectorFacts{}
	e, ok := detectorEndpointFor(id, target)
	if !ok || !validDetectorTarget(target, family) {
		return facts, errors.New("invalid fetch arguments")
	}
	file, err := os.OpenFile(dir+"/"+id+".body", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return facts, err
	}
	defer file.Close()
	local := *client
	local.Jar = nil
	local.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !allowedDetectorURL(e, req.URL) {
			return errServiceRedirect
		}
		req.Header.Del("Cookie")
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.URL, nil)
	if err != nil {
		return facts, err
	}
	req.Header.Set("User-Agent", serviceUserAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Accept", "text/html,application/json;q=0.9,*/*;q=0.8")
	if id == "openai-cookie" {
		req.Header.Set("Origin", "https://platform.openai.com")
		req.Header.Set("Referer", "https://platform.openai.com/")
	}
	response, err := local.Do(req)
	if err != nil {
		facts.ErrorCode = errorCodeForHTTPError(err)
		if errors.Is(err, errServiceRedirect) {
			facts.ErrorCode = "unexpected_redirect"
		}
		return facts, nil
	}
	defer response.Body.Close()
	facts.HTTPStatus = response.StatusCode
	var reader io.Reader = response.Body
	if !response.Uncompressed && strings.EqualFold(strings.TrimSpace(response.Header.Get("Content-Encoding")), "gzip") {
		zipped, err := gzip.NewReader(reader)
		if err != nil {
			facts.ErrorCode = "invalid_response"
			return facts, nil
		}
		defer zipped.Close()
		reader = zipped
	}
	n, err := io.Copy(file, io.LimitReader(reader, e.Limit+1))
	if ctx.Err() != nil {
		facts.ErrorCode = errorCodeForHTTPError(ctx.Err())
		return facts, nil
	}
	if err != nil {
		facts.ErrorCode = "invalid_response"
		return facts, nil
	}
	if n > e.Limit {
		facts.ErrorCode = "response_too_large"
		return facts, nil
	}
	if err = file.Close(); err != nil {
		return facts, err
	}
	body, err := os.ReadFile(dir + "/" + id + ".body")
	if err != nil {
		return facts, err
	}
	final := req.URL
	if response.Request != nil {
		final = response.Request.URL
	}
	if !allowedDetectorURL(e, final) {
		facts.ErrorCode = "unexpected_redirect"
		return facts, nil
	}
	facts = extractDetectorFacts(id, response.StatusCode, final, body)
	if facts.JSON != nil && (id == "geo-maxmind" || id == "geo-ipinfo") {
		payload := facts.JSON
		returned := make([]any, 0, 5)
		if id == "geo-ipinfo" {
			data, _ := payload["data"].(map[string]any)
			returned = append(returned, data["ip"])
		} else {
			for _, key := range []string{"ip", "IP", "ip_address", "query"} {
				if value, exists := payload[key]; exists {
					returned = append(returned, value)
				}
			}
			if traits, ok := payload["traits"].(map[string]any); ok {
				if value, exists := traits["ip_address"]; exists {
					returned = append(returned, value)
				}
			}
		}
		for _, value := range returned {
			text, ok := value.(string)
			ip, err := netip.ParseAddr(text)
			if !ok || err != nil || ip.String() != target {
				facts.ErrorCode = "ip_mismatch"
				break
			}
		}
	}
	return facts, nil
}

// RunIPQualityFetch is a closed private subcommand. It must be dispatched before
// loading configuration or credentials. No URL, header, output path or config is accepted.
func RunIPQualityFetch(args []string) int {
	if len(args) != 3 {
		return 2
	}
	family, err := strconv.Atoi(args[2])
	if err != nil || !validDetectorTarget(args[1], family) {
		return 2
	}
	if _, ok := detectorEndpointFor(args[0], args[1]); !ok {
		return 2
	}
	dir, err := os.Getwd()
	if err != nil {
		return 2
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return 2
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Getuid()) {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client := detectorHTTPClient(family)
	defer client.CloseIdleConnections()
	facts, err := fetchDetectorEndpoint(ctx, client, args[0], args[1], family, dir)
	if err != nil {
		return 1
	}
	file, err := os.OpenFile(args[0]+".json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return 1
	}
	err = json.NewEncoder(file).Encode(facts)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return 1
	}
	// Small, bounded metadata only; the body and static objects never enter argv/stdout.
	fmt.Printf("{\"endpoint\":%q,\"http_status\":%d,\"error_code\":%q}\n", args[0], facts.HTTPStatus, facts.ErrorCode)
	return 0
}
