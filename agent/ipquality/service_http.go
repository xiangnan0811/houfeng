package ipquality

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const serviceBodyLimit = 2 * 1024 * 1024
const serviceUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"

var (
	errServiceBodyLimit = errors.New("service response exceeds size limit")
	errServiceRedirect  = errors.New("service redirect is not allowed")
)

type serviceHTTPResponse struct {
	StatusCode int
	Body       []byte
	FinalURL   *url.URL
}

func serviceHosts(service string) []string {
	switch service {
	case "netflix":
		return []string{"netflix.com", "www.netflix.com"}
	case "chatgpt":
		return []string{"api.openai.com", "ios.chat.openai.com"}
	case "youtube-premium":
		return []string{"www.youtube.com", "consent.youtube.com", "consent.google.com"}
	case "amazon-prime-video":
		return []string{"www.primevideo.com", "primevideo.com"}
	case "disney-plus":
		return []string{"disney.api.edge.bamgrid.com"}
	case "tiktok":
		return []string{"www.tiktok.com", "tiktok.com"}
	case "reddit":
		return []string{"www.reddit.com", "reddit.com"}
	default:
		return nil
	}
}

func allowedServiceURL(service string, target *url.URL) bool {
	if target == nil || target.Scheme != "https" || target.User != nil || (target.Port() != "" && target.Port() != "443") {
		return false
	}
	for _, host := range serviceHosts(service) {
		if strings.EqualFold(target.Hostname(), host) {
			return true
		}
	}
	return false
}

// Each request copies client policy, never the transport. Cookies and credentials
// from other requests must not affect an anonymous probe.
func (c *HTTPCollector) doServiceRequest(ctx context.Context, ipVersion int, service, method, endpoint string, headers http.Header, body io.Reader) (serviceHTTPResponse, error) {
	var result serviceHTTPResponse
	if err := ctx.Err(); err != nil {
		return result, err
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return result, err
	}
	if !allowedServiceURL(service, req.URL) {
		return result, errServiceRedirect
	}
	req.Header = headers.Clone()
	if req.Header == nil {
		req.Header = make(http.Header)
	}
	req.Header.Set("User-Agent", serviceUserAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Del("Cookie")
	client := *c.serviceHTTPClient(ipVersion)
	client.Jar = nil
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) > 5 || !allowedServiceURL(service, next.URL) || (service == "disney-plus" && method == http.MethodPost) {
			return errServiceRedirect
		}
		if !strings.EqualFold(next.URL.Host, req.URL.Host) {
			next.Header.Del("Authorization")
		}
		next.Header.Del("Cookie")
		return nil
	}
	response, err := client.Do(req)
	if err != nil {
		return result, err
	}
	defer response.Body.Close()
	result.StatusCode = response.StatusCode
	result.FinalURL = req.URL
	if response.Request != nil {
		result.FinalURL = response.Request.URL
	}
	var reader io.Reader = response.Body
	if !response.Uncompressed && strings.EqualFold(strings.TrimSpace(response.Header.Get("Content-Encoding")), "gzip") {
		decoded, err := gzip.NewReader(response.Body)
		if err != nil {
			return result, err
		}
		defer decoded.Close()
		reader = decoded
	}
	result.Body, err = io.ReadAll(io.LimitReader(reader, serviceBodyLimit+1))
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err != nil {
		return result, err
	}
	if len(result.Body) > serviceBodyLimit {
		result.Body = nil
		return result, errServiceBodyLimit
	}
	return result, nil
}
