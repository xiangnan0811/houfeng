package ipquality

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
	"houfeng/internal/contracts/agentapi"
)

func (c *HTTPCollector) collectDefaultServiceUnlocks(ctx context.Context, services []string, ipVersion int) []serviceProbeOutcome {
	outcomes := make([]serviceProbeOutcome, len(services))
	var workers sync.WaitGroup
	workerCount := min(len(services), 7)
	workers.Add(workerCount)
	for worker := range workerCount {
		go func(worker int) {
			defer workers.Done()
			for index := worker; index < len(services); index += workerCount {
				serviceCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				outcomes[index] = c.probeService(serviceCtx, services[index], ipVersion)
				cancel()
			}
		}(worker)
	}
	workers.Wait()
	return outcomes
}

// Evidence contains only rules, response statuses and normalized countries; never
// response bodies, URLs, cookies, or anonymous device credentials.
type serviceEvidence struct {
	ProbeRevision int                       `json:"probe_revision"`
	EvidenceKind  string                    `json:"evidence_kind"`
	MatchedRule   string                    `json:"matched_rule,omitempty"`
	HTTPStatus    int                       `json:"http_status,omitempty"`
	Region        string                    `json:"region,omitempty"`
	IPVersion     int                       `json:"ip_version"`
	Endpoints     []serviceEndpointEvidence `json:"endpoints,omitempty"`
}
type serviceEndpointEvidence struct {
	Endpoint    string `json:"endpoint"`
	HTTPStatus  int    `json:"http_status,omitempty"`
	MatchedRule string `json:"matched_rule,omitempty"`
}
type serviceDecision struct{ status, region, scope, rule, code string }
type serviceProbe struct {
	collector *HTTPCollector
	ctx       context.Context
	service   string
	ipVersion int
	evidence  serviceEvidence
}

func (c *HTTPCollector) probeService(ctx context.Context, service string, ipVersion int) serviceProbeOutcome {
	source, ok := defaultServiceSource(service)
	if !ok {
		return unsupportedServiceOutcome(service)
	}
	start := time.Now()
	probe := serviceProbe{collector: c, ctx: ctx, service: service, ipVersion: ipVersion, evidence: serviceEvidence{ProbeRevision: 2, EvidenceKind: "anonymous_service_probe", IPVersion: ipVersion}}
	var decision serviceDecision
	if err := ctx.Err(); err != nil {
		decision.code = errorCodeForHTTPError(err)
	} else {
		switch service {
		case "netflix":
			decision = probe.netflix()
		case "chatgpt":
			decision = probe.chatGPT()
		case "youtube-premium":
			decision = probe.youtube()
		case "amazon-prime-video":
			decision = probe.prime()
		case "disney-plus":
			decision = probe.disney()
		case "tiktok":
			decision = probe.tiktok()
		case "reddit":
			decision = probe.reddit()
		}
	}
	probe.evidence.MatchedRule = decision.rule
	var outcome serviceProbeOutcome
	if decision.code != "" || decision.status == "" {
		code := firstNonEmpty(decision.code, "invalid_response")
		outcome = serviceFailure(service, source, code, serviceErrorSummary(code), elapsedMillis(start), nil)
	} else {
		probe.evidence.Region = decision.region
		outcome.Result = agentapi.IPQualityServiceUnlockPayload{Service: service, Source: source, Status: decision.status, ProbeStatus: sourceStatusSuccess, Region: decision.region, UnlockType: decision.scope, LatencyMS: elapsedMillis(start)}
	}
	outcome.Raw = extraJSON(probe.evidence)
	outcome.Result.ExtraJSON = outcome.Raw
	return outcome
}

func serviceErrorSummary(code string) string {
	switch code {
	case "response_too_large":
		return "service response exceeds size limit"
	case "unexpected_redirect":
		return "service redirect is not allowed"
	case "challenge_detected":
		return "service challenge detected"
	case "egress_ambiguous":
		return "service egress is ambiguous"
	case "invalid_response":
		return "invalid service response"
	case "http_status":
		return "service returned an unsuccessful HTTP status"
	default:
		return "service request failed"
	}
}

func (p *serviceProbe) request(label, method, endpoint string, headers http.Header, body io.Reader) (serviceHTTPResponse, string) {
	response, err := p.collector.doServiceRequest(p.ctx, p.ipVersion, p.service, method, endpoint, headers, body)
	p.evidence.HTTPStatus = response.StatusCode
	p.evidence.Endpoints = append(p.evidence.Endpoints, serviceEndpointEvidence{Endpoint: label, HTTPStatus: response.StatusCode})
	if err == nil {
		return response, ""
	}
	if errors.Is(err, errServiceBodyLimit) {
		return response, "response_too_large"
	}
	if errors.Is(err, errServiceRedirect) {
		return response, "unexpected_redirect"
	}
	return response, errorCodeForHTTPError(err)
}
func unknownService(code string) serviceDecision { return serviceDecision{code: code} }
func knownService(status, region, scope, rule string) serviceDecision {
	return serviceDecision{status: status, region: region, scope: scope, rule: rule}
}
func successfulHTTP(response serviceHTTPResponse) bool {
	return response.StatusCode >= 200 && response.StatusCode < 300
}
func fatalHTTP(response serviceHTTPResponse) bool {
	return response.StatusCode == 429 || response.StatusCode >= 500
}
func loginServiceURL(target *url.URL) bool {
	if target == nil {
		return false
	}
	for _, segment := range strings.Split(strings.ToLower(target.Path), "/") {
		if segment == "login" || segment == "signin" || segment == "sign-in" {
			return true
		}
	}
	return false
}

// HTML text is interpreted structurally: script/style, template and hidden
// subtrees do not provide visible success or restriction text.
type servicePage struct {
	title, text       string
	visibleMarkup     bool
	headings          []string
	metas             map[string]string
	scripts           []string
	objects           []any
	challenge         bool
	login             bool
	shredditCountries []string
}

func foldServiceText(value string) string { return strings.Join(strings.Fields(value), " ") }
func serviceCountry(value any) string {
	text, ok := value.(string)
	if !ok || len(text) != 2 {
		return ""
	}
	for _, char := range text {
		if !((char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')) {
			return ""
		}
	}
	return strings.ToUpper(text)
}
func parseServicePage(body []byte) servicePage {
	page := servicePage{metas: make(map[string]string)}
	tokenizer := html.NewTokenizer(bytes.NewReader(body))
	type frame struct {
		tag    string
		hidden bool
	}
	var stack []frame
	var visible, title, heading, script strings.Builder
	inTitle, inHeading, inScript := false, false, false
	for {
		typeOfToken := tokenizer.Next()
		if typeOfToken == html.ErrorToken {
			break
		}
		token := tokenizer.Token()
		switch typeOfToken {
		case html.StartTagToken, html.SelfClosingTagToken:
			tag := token.Data
			attrs := make(map[string]string, len(token.Attr))
			for _, attr := range token.Attr {
				attrs[attr.Key] = attr.Val
			}
			hidden := len(stack) > 0 && stack[len(stack)-1].hidden
			_, hasHidden := attrs["hidden"]
			hidden = hidden || hasHidden || attrs["aria-hidden"] == "true" || tag == "script" || tag == "style" || tag == "template" || tag == "noscript"
			inlineStyle := strings.ToLower(strings.Join(strings.Fields(attrs["style"]), ""))
			hidden = hidden || strings.Contains(inlineStyle, "display:none") || strings.Contains(inlineStyle, "visibility:hidden")
			if !hidden && tag != "meta" && tag != "link" {
				page.visibleMarkup = true
			}
			if !hidden && tag == "input" && strings.EqualFold(attrs["type"], "password") {
				page.login = true
			}
			if !hidden && tag == "form" {
				action, err := url.Parse(attrs["action"])
				if err == nil && loginServiceURL(action) {
					page.login = true
				}
			}
			for _, key := range []string{"id", "class", "src", "action"} {
				value := strings.ToLower(attrs[key])
				if strings.Contains(value, "cf-chl-") || strings.Contains(value, "challenge-platform") || strings.Contains(value, "g-recaptcha") || strings.Contains(value, "hcaptcha") {
					page.challenge = true
				}
			}
			if tag == "meta" {
				page.metas[strings.ToLower(firstNonEmpty(attrs["property"], attrs["name"]))] = attrs["content"]
			}
			if strings.HasPrefix(tag, "shreddit-") && !hidden {
				if country := serviceCountry(attrs["country"]); country != "" {
					page.shredditCountries = append(page.shredditCountries, country)
				}
			}
			if tag == "title" && !hidden {
				inTitle = true
			}
			if tag == "h1" && !hidden {
				inHeading = true
				heading.Reset()
			}
			if tag == "script" {
				inScript = true
				script.Reset()
			}
			if typeOfToken == html.StartTagToken && !voidServiceTag(tag) {
				stack = append(stack, frame{tag: tag, hidden: hidden})
			}
			if !hidden {
				visible.WriteByte(' ')
			}
		case html.EndTagToken:
			if token.Data == "title" {
				inTitle = false
			}
			if token.Data == "h1" && inHeading {
				page.headings = append(page.headings, foldServiceText(heading.String()))
				inHeading = false
			}
			if token.Data == "script" && inScript {
				value := script.String()
				page.scripts = append(page.scripts, value)
				page.objects = append(page.objects, serviceScriptObjects(value)...)
				lower := strings.ToLower(value)
				if strings.Contains(lower, "cf-chl-") || strings.Contains(lower, "challenge-platform") {
					page.challenge = true
				}
				inScript = false
			}
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].tag == token.Data {
					stack = stack[:i]
					break
				}
			}
			visible.WriteByte(' ')
		case html.TextToken:
			if inScript {
				script.WriteString(token.Data)
				continue
			}
			if len(stack) > 0 && stack[len(stack)-1].hidden {
				continue
			}
			visible.WriteString(token.Data)
			if inTitle {
				title.WriteString(token.Data)
			}
			if inHeading {
				heading.WriteString(token.Data)
			}
		}
	}
	page.title = foldServiceText(title.String())
	switch strings.ToLower(page.title) {
	case "login", "log in", "sign in":
		page.login = true
	}
	page.text = foldServiceText(visible.String())
	lower := strings.ToLower(page.text)
	if strings.Contains(lower, "verify you are human") || strings.Contains(lower, "just a moment...") {
		page.challenge = true
	}
	return page
}
func voidServiceTag(tag string) bool {
	switch tag {
	case "area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "param", "source", "track", "wbr":
		return true
	}
	return false
}

// Extract balanced JSON objects from script wrappers without executing JavaScript.
// Decoding the outer object also preserves relationships such as Netflix's
// country id/name pair; string escapes are handled by encoding/json.
func serviceScriptObjects(script string) []any {
	var result []any
	start, depth := -1, 0
	var wrapperQuote byte
	wrapperEscaped, lineComment, blockComment := false, false, false
	blockOpening, blockStar := false, false
	regexLiteral, regexClass, regexEscaped := false, false, false
	regexOperandStart, memberName := true, false
	for i := 0; i < len(script); i++ {
		char := script[i]
		if lineComment {
			if char == '\n' || char == '\r' {
				lineComment = false
			}
			continue
		}
		if blockComment {
			if blockOpening {
				blockOpening = false
				continue
			}
			if char == '/' && blockStar {
				blockComment = false
			}
			blockStar = char == '*'
			continue
		}
		if wrapperQuote != 0 {
			if wrapperEscaped {
				wrapperEscaped = false
			} else if char == '\\' {
				wrapperEscaped = true
			} else if char == wrapperQuote {
				wrapperQuote = 0
			}
			continue
		}
		if regexLiteral {
			if regexEscaped {
				regexEscaped = false
				continue
			}
			if char == '\\' {
				regexEscaped = true
				continue
			}
			if char == '[' {
				regexClass = true
			}
			if char == ']' {
				regexClass = false
			}
			if char == '/' && !regexClass {
				regexLiteral = false
			}
			continue
		}
		if char == ' ' || char == '\t' || char == '\n' || char == '\r' {
			continue
		}
		// Consume whole words so expression keywords differ from identifiers
		// and property names (obj.return is an operand, not a return keyword).
		if serviceScriptWordByte(char) {
			wordStart := i
			for i+1 < len(script) && serviceScriptWordByte(script[i+1]) {
				i++
			}
			regexOperandStart = false
			if !memberName {
				switch script[wordStart : i+1] {
				case "return", "throw", "case", "yield", "await", "delete", "void", "typeof", "new", "in", "instanceof":
					regexOperandStart = true
				}
			}
			memberName = false
			continue
		}
		switch char {
		case '\'', '"', '`':
			wrapperQuote = char
			regexOperandStart = false
		case '/':
			if i+1 < len(script) && script[i+1] == '/' {
				lineComment = true
			} else if i+1 < len(script) && script[i+1] == '*' {
				blockComment = true
				blockOpening = true
				blockStar = false
			} else if regexOperandStart {
				regexLiteral = true
				regexClass = false
				regexOperandStart = false
			} else {
				// Division consumes an operand and expects another; that
				// operand may itself be a regex literal.
				regexOperandStart = true
			}
		case '{', '[':
			if depth == 0 {
				start = i
			}
			depth++
			regexOperandStart = true
		case '}', ']':
			if depth > 0 {
				depth--
				if depth == 0 {
					var value any
					if json.Unmarshal([]byte(script[start:i+1]), &value) == nil {
						result = append(result, value)
					}
				}
			}
			regexOperandStart = false
		case '.':
			memberName = true
			regexOperandStart = false
			continue
		case '+', '-':
			if i+1 < len(script) && script[i+1] == char {
				// Prefix ++/-- still expects an operand; postfix ++/--
				// still ends one. Do not turn postfix division into regex.
				i++
			} else {
				regexOperandStart = true
			}
		default:
			regexOperandStart = strings.ContainsRune("=(:,;?!~&|^%*<>", rune(char))
		}
		if !lineComment && !blockComment {
			memberName = false
		}
	}
	return result
}

func serviceScriptWordByte(char byte) bool {
	return char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' ||
		char >= '0' && char <= '9' || char == '_' || char == '$' || char >= 0x80
}
func serviceJSON(body []byte) (map[string]any, bool) {
	var value map[string]any
	if json.Unmarshal(body, &value) != nil || value == nil {
		return nil, false
	}
	return value, true
}
func visitServiceObjects(value any, visit func(map[string]any)) {
	switch node := value.(type) {
	case map[string]any:
		visit(node)
		for _, child := range node {
			visitServiceObjects(child, visit)
		}
	case []any:
		for _, child := range node {
			visitServiceObjects(child, visit)
		}
	}
}
func serviceValues(objects []any, key string) []any {
	var values []any
	for _, object := range objects {
		visitServiceObjects(object, func(node map[string]any) {
			if value, exists := node[key]; exists {
				values = append(values, value)
			}
		})
	}
	return values
}
func serviceRegion(objects []any, key string) (string, bool) {
	region := ""
	for _, value := range serviceValues(objects, key) {
		country := serviceCountry(value)
		if country == "" || (region != "" && region != country) {
			return "", false
		}
		region = country
	}
	return region, true
}
func serviceBool(objects []any, key string) (*bool, bool) {
	var result *bool
	for _, value := range serviceValues(objects, key) {
		flag, ok := value.(bool)
		if !ok || (result != nil && *result != flag) {
			return nil, false
		}
		copyOfFlag := flag
		result = &copyOfFlag
	}
	return result, true
}
func pageFailure(response serviceHTTPResponse, page servicePage) string {
	if fatalHTTP(response) {
		return "http_status"
	}
	if page.challenge {
		return "challenge_detected"
	}
	if page.login || loginServiceURL(response.FinalURL) {
		return "invalid_response"
	}
	return ""
}

func (p *serviceProbe) netflix() serviceDecision {
	titles := []struct{ id, name string }{{"81280792", "LEGO Ninjago"}, {"70143836", "Breaking Bad"}}
	valid, unavailable, region := 0, 0, ""
	for _, title := range titles {
		response, code := p.request("title_"+title.id, http.MethodGet, "https://www.netflix.com/title/"+title.id, nil, nil)
		if code != "" {
			return unknownService(code)
		}
		page := parseServicePage(response.Body)
		if code := pageFailure(response, page); code != "" {
			return unknownService(code)
		}
		branded := strings.EqualFold(strings.TrimSpace(page.metas["al:android:app_name"]), "Netflix")
		hasHeading := false
		for _, heading := range page.headings {
			if strings.EqualFold(heading, title.name) {
				hasHeading = true
			}
		}
		android, err := url.Parse(page.metas["al:android:url"])
		matchesID := err == nil && android != nil && (strings.TrimRight(android.Path, "/") == "/title/"+title.id || (android.Host == "title" && android.Path == "/"+title.id))
		if successfulHTTP(response) && branded && matchesID && hasHeading {
			valid++
		} else if branded && !hasHeading && strings.Contains(strings.ToLower(page.text), "oh no!") {
			unavailable++
		} else {
			if !successfulHTTP(response) {
				return unknownService("http_status")
			}
			return unknownService("invalid_response")
		}
		for _, object := range page.objects {
			conflict := false
			visitServiceObjects(object, func(node map[string]any) {
				name, nameOK := node["countryName"].(string)
				country := serviceCountry(node["id"])
				if !nameOK || strings.TrimSpace(name) == "" || country == "" {
					return
				}
				if region != "" && region != country {
					conflict = true
				}
				region = country
			})
			if conflict {
				return unknownService("invalid_response")
			}
		}
	}
	if valid > 0 {
		return knownService("unlocked", region, "catalog_sample", "netflix_title_catalog")
	}
	if unavailable == 2 {
		return knownService("partial", region, "sample_titles_unavailable", "netflix_titles_oh_no")
	}
	return unknownService("invalid_response")
}

func (p *serviceProbe) chatGPT() serviceDecision {
	header := http.Header{"Accept": {"application/json"}, "Origin": {"https://platform.openai.com"}, "Referer": {"https://platform.openai.com/"}}
	web, code := p.request("web_cookie_requirements", http.MethodGet, "https://api.openai.com/compliance/cookie_requirements", header, nil)
	webState, webCode := "unknown", code
	if code == "" {
		page := parseServicePage(web.Body)
		webCode = pageFailure(web, page)
		if webCode == "" {
			value, ok := serviceJSON(web.Body)
			if ok {
				errObject, _ := value["error"].(map[string]any)
				if unsupportedCountryError(errObject) {
					webState = "restricted"
				} else if _, ok := value["cookie_consent_required"].(bool); ok && successfulHTTP(web) {
					webState = "positive"
				}
			}
			if webState == "unknown" {
				webCode = "invalid_response"
				if !successfulHTTP(web) {
					webCode = "http_status"
				}
			}
		}
	}
	app, code := p.request("app_home", http.MethodGet, "https://ios.chat.openai.com/", nil, nil)
	appState, appCode := "unknown", code
	if code == "" {
		page := parseServicePage(app.Body)
		appCode = pageFailure(app, page)
		if appCode == "" {
			text := strings.ToLower(page.text)
			branded := strings.Contains(text, "chatgpt") || strings.Contains(text, "openai")
			if branded && page.visibleMarkup {
				for _, refusal := range []string{"disable your vpn", "turn off your vpn", "vpn is not supported", "vpn is not allowed"} {
					if strings.Contains(text, refusal) {
						appState = "restricted"
					}
				}
				if appState == "unknown" && successfulHTTP(app) {
					appState = "positive"
				}
			}
			if appState == "unknown" {
				appCode = "invalid_response"
				if !successfulHTTP(app) {
					appCode = "http_status"
				}
			}
		}
	}
	if webState == "positive" && appState == "positive" {
		return knownService("unlocked", "", "web_and_app", "openai_web_and_app")
	}
	if webState == "positive" {
		if appState == "restricted" {
			return knownService("partial", "", "web_only", "openai_web_positive_app_restricted")
		}
		return knownService("partial", "", "web_verified_app_unknown", "openai_web_positive_app_unknown")
	}
	if appState == "positive" {
		if webState == "restricted" {
			return knownService("partial", "", "app_only", "openai_app_positive_web_restricted")
		}
		return knownService("partial", "", "app_verified_web_unknown", "openai_app_positive_web_unknown")
	}
	if webState == "restricted" && appState == "restricted" {
		return knownService("blocked", "", "web_and_app", "openai_both_restricted")
	}
	return unknownService(firstNonEmpty(webCode, appCode, "invalid_response"))
}
func unsupportedCountryError(value map[string]any) bool {
	for _, key := range []string{"code", "type"} {
		text, _ := value[key].(string)
		if text == "unsupported_country" || text == "unsupported_country_region_territory" {
			return true
		}
	}
	return false
}

func (p *serviceProbe) youtube() serviceDecision {
	response, code := p.request("premium", http.MethodGet, "https://www.youtube.com/premium", nil, nil)
	if code != "" {
		return unknownService(code)
	}
	page := parseServicePage(response.Body)
	if code := pageFailure(response, page); code != "" {
		return unknownService(code)
	}
	if response.FinalURL != nil && (response.FinalURL.Hostname() == "consent.youtube.com" || response.FinalURL.Hostname() == "consent.google.com") {
		return unknownService("invalid_response")
	}
	valid := false
	for _, script := range page.scripts {
		if strings.Contains(script, "ytcfg") || strings.Contains(script, "ytInitialData") {
			valid = true
		}
	}
	if !valid {
		return unknownService("invalid_response")
	}
	if strings.Contains(strings.ToLower(page.text), "premium is not available in your country") {
		return knownService("blocked", "", "premium_offer", "youtube_premium_country_restriction")
	}
	if !successfulHTTP(response) {
		return unknownService("http_status")
	}
	region, ok := serviceRegion(page.objects, "contentRegion")
	if !ok {
		return unknownService("invalid_response")
	}
	if region == "" {
		region, ok = serviceRegion(page.objects, "INNERTUBE_CONTEXT_GL")
	}
	if !ok || region == "" || !strings.Contains(strings.ToLower(page.text), "ad-free") {
		return unknownService("invalid_response")
	}
	return knownService("unlocked", region, "premium_offer", "youtube_premium_region_offer")
}

func (p *serviceProbe) prime() serviceDecision {
	response, code := p.request("home", http.MethodGet, "https://www.primevideo.com/", nil, nil)
	if code != "" {
		return unknownService(code)
	}
	page := parseServicePage(response.Body)
	if code := pageFailure(response, page); code != "" {
		return unknownService(code)
	}
	if !strings.Contains(strings.ToLower(page.title), "prime video") {
		return unknownService("invalid_response")
	}
	restricted, ok := serviceBool(page.objects, "isServiceRestricted")
	if !ok {
		return unknownService("invalid_response")
	}
	if restricted != nil && *restricted {
		return knownService("blocked", "", "territory_offer", "prime_service_restricted")
	}
	if !successfulHTTP(response) {
		return unknownService("http_status")
	}
	region, ok := serviceRegion(page.objects, "currentTerritory")
	if !ok || region == "" {
		return unknownService("invalid_response")
	}
	return knownService("unlocked", region, "territory_offer", "prime_current_territory")
}

const disneyBrowserClient = "ZGlzbmV5JmJyb3dzZXImMS4wLjA.Cu56AgSfBTDag5NiRA81oLHkDZfu5L3CKadnefEAY84"

func (p *serviceProbe) disneyJSON(label, path, contentType, authorization string, body io.Reader) (map[string]any, serviceDecision) {
	header := http.Header{"Content-Type": {contentType}, "Authorization": {authorization}, "Accept": {"application/json"}}
	response, code := p.request(label, http.MethodPost, "https://disney.api.edge.bamgrid.com"+path, header, body)
	if code != "" {
		return nil, unknownService(code)
	}
	page := parseServicePage(response.Body)
	if code := pageFailure(response, page); code != "" {
		return nil, unknownService(code)
	}
	value, ok := serviceJSON(response.Body)
	if ok && value["error_description"] == "forbidden-location" {
		return nil, knownService("blocked", "", "supported_location", "disney_forbidden_location")
	}
	if !successfulHTTP(response) {
		return nil, unknownService("http_status")
	}
	if !ok {
		return nil, unknownService("invalid_response")
	}
	return value, serviceDecision{}
}
func (p *serviceProbe) disney() serviceDecision {
	device, decision := p.disneyJSON("devices", "/devices", "application/json", "Bearer "+disneyBrowserClient, strings.NewReader(`{"deviceFamily":"browser","applicationRuntime":"chrome","deviceProfile":"windows","attributes":{}}`))
	if device == nil {
		return decision
	}
	assertion, ok := device["assertion"].(string)
	if !ok || strings.TrimSpace(assertion) == "" {
		return unknownService("invalid_response")
	}
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:token-exchange"}, "latitude": {"0"}, "longitude": {"0"}, "platform": {"browser"}, "subject_token": {assertion}, "subject_token_type": {"urn:bamtech:params:oauth:token-type:device"}}
	token, decision := p.disneyJSON("token", "/token", "application/x-www-form-urlencoded", "Bearer "+disneyBrowserClient, strings.NewReader(form.Encode()))
	if token == nil {
		return decision
	}
	refresh, ok := token["refresh_token"].(string)
	if !ok || strings.TrimSpace(refresh) == "" {
		return unknownService("invalid_response")
	}
	body, err := json.Marshal(map[string]any{"query": "mutation refreshToken($input: RefreshTokenInput!) { refreshToken(refreshToken: $input) { activeSession { sessionId } } }", "variables": map[string]any{"input": map[string]any{"refreshToken": refresh}}})
	if err != nil {
		return unknownService("invalid_response")
	}
	graph, decision := p.disneyJSON("device_graphql", "/graph/v1/device/graphql", "application/json", disneyBrowserClient, bytes.NewReader(body))
	if graph == nil {
		return decision
	}
	if graphErrors, exists := graph["errors"]; exists && graphErrors != nil {
		list, ok := graphErrors.([]any)
		if !ok || len(list) > 0 {
			return unknownService("invalid_response")
		}
	}
	extensions, _ := graph["extensions"].(map[string]any)
	sdk, _ := extensions["sdk"].(map[string]any)
	session, _ := sdk["session"].(map[string]any)
	location, _ := session["location"].(map[string]any)
	country := serviceCountry(location["countryCode"])
	supported, ok := session["inSupportedLocation"].(bool)
	if !ok || country == "" {
		return unknownService("invalid_response")
	}
	if supported {
		return knownService("unlocked", country, "supported_location", "disney_supported_location")
	}
	return knownService("blocked", country, "supported_location", "disney_unsupported_location")
}

func (p *serviceProbe) tiktok() serviceDecision {
	response, code := p.request("home", http.MethodGet, "https://www.tiktok.com/", nil, nil)
	if code != "" {
		return unknownService(code)
	}
	page := parseServicePage(response.Body)
	if code := pageFailure(response, page); code != "" {
		return unknownService(code)
	}
	if strings.Contains(strings.ToLower(page.text), "please wait...") {
		response, code = p.request("explore", http.MethodGet, "https://www.tiktok.com/explore", nil, nil)
		if code != "" {
			return unknownService(code)
		}
		page = parseServicePage(response.Body)
		if code := pageFailure(response, page); code != "" {
			return unknownService(code)
		}
	}
	if !strings.Contains(strings.ToLower(page.title), "tiktok") {
		return unknownService("invalid_response")
	}
	text := strings.ToLower(page.text)
	if strings.Contains(text, "not available in your country") || strings.Contains(text, "not available in your region") {
		return knownService("blocked", "", "web_region", "tiktok_region_restriction")
	}
	if !successfulHTTP(response) {
		return unknownService("http_status")
	}
	region, ok := serviceRegion(page.objects, "region")
	if !ok || region == "" {
		return unknownService("invalid_response")
	}
	return knownService("unlocked", region, "web_region", "tiktok_script_region")
}

func (p *serviceProbe) reddit() serviceDecision {
	response, code := p.request("anonymous_chat", http.MethodGet, "https://www.reddit.com/svc/shreddit/reddit-chat", nil, nil)
	if code != "" {
		return unknownService(code)
	}
	page := parseServicePage(response.Body)
	if code := pageFailure(response, page); code != "" {
		return unknownService(code)
	}
	if page.visibleMarkup && strings.Contains(strings.ToLower(page.text), "blocked by network security") {
		return knownService("blocked", "", "anonymous_web", "reddit_network_security")
	}
	if !successfulHTTP(response) {
		return unknownService("http_status")
	}
	region := ""
	for _, country := range page.shredditCountries {
		if region != "" && region != country {
			return unknownService("invalid_response")
		}
		region = country
	}
	if region == "" {
		return unknownService("invalid_response")
	}
	return knownService("unlocked", region, "anonymous_web", "reddit_shreddit_country")
}

func unsupportedServiceOutcome(service string) serviceProbeOutcome {
	return serviceProbeOutcome{Result: agentapi.IPQualityServiceUnlockPayload{Service: service, Source: "default_probe_registry", Status: "unknown", ProbeStatus: sourceStatusSkipped, ErrorCode: "unsupported_service", ErrorSummary: "service is not supported by default IP quality probes"}}
}
func ambiguousServiceOutcomes(services []string) []serviceProbeOutcome {
	outcomes := make([]serviceProbeOutcome, len(services))
	for index, service := range services {
		source, ok := defaultServiceSource(service)
		if !ok {
			outcomes[index] = unsupportedServiceOutcome(service)
			continue
		}
		outcomes[index] = serviceFailure(service, source, "egress_ambiguous", "service egress is ambiguous", nil, nil)
	}
	return outcomes
}
func defaultServiceSource(service string) (string, bool) {
	switch service {
	case "netflix":
		return "netflix_title_probe", true
	case "chatgpt":
		return "openai_status_probe", true
	case "youtube-premium":
		return "youtube_premium_page_probe", true
	case "amazon-prime-video":
		return "prime_video_page_probe", true
	case "disney-plus":
		return "disney_default_probe", true
	case "tiktok":
		return "tiktok_home_probe", true
	case "reddit":
		return "reddit_home_probe", true
	default:
		return "", false
	}
}
