package execrt

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/dushibing/feishu-plugin-platform/internal/shortcut"
)

// Engine interprets a FieldShortcut at request time. It is safe for concurrent
// use. The zero value is not usable — call New.
type Engine struct {
	client       *http.Client
	maxBodyBytes int64
}

// Options configure the runtime's safety envelope.
type Options struct {
	Timeout      time.Duration // per outbound request (default 10s)
	MaxBodyBytes int64         // cap on a single response body (default 1MiB)
	AllowPrivate bool          // allow outbound to private/loopback IPs (default false; SSRF guard)
}

// New builds an Engine. Outbound requests get a per-request timeout and, unless
// AllowPrivate is set, a dialer that refuses private / loopback / link-local
// addresses (SSRF guard layered under the domain allowlist).
func New(o Options) *Engine {
	if o.Timeout <= 0 {
		o.Timeout = 10 * time.Second
	}
	if o.MaxBodyBytes <= 0 {
		o.MaxBodyBytes = 1 << 20
	}
	dialer := &net.Dialer{Timeout: o.Timeout}
	tr := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment, // honor an egress allowlist proxy (HTTP_PROXY)
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          50,
		MaxConnsPerHost:       16, // bound concurrent dials per host (DoS-amplification floor)
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   o.Timeout,
		ExpectContinueTimeout: time.Second,
	}
	if !o.AllowPrivate {
		// When an egress proxy is configured (the intended production egress-
		// control path), the transport dials the PROXY's address — which is
		// itself usually private/in-cluster. Allow dialing the configured proxy
		// (it is the egress control point + the per-plugin host allowlist still
		// applies); block every other private/loopback/link-local target (SSRF).
		proxies := proxyAddrs()
		tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if !proxies[addr] {
				if host, _, err := net.SplitHostPort(addr); err == nil {
					if ip := net.ParseIP(host); ip != nil && isPrivateIP(ip) {
						return nil, fmt.Errorf("blocked outbound to private address %s", host)
					}
				}
			}
			c, err := dialer.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			// Resolved-name path: verify the chosen IP is public (unless this is
			// the configured proxy).
			if !proxies[addr] {
				if tcp, ok := c.RemoteAddr().(*net.TCPAddr); ok && isPrivateIP(tcp.IP) {
					c.Close()
					return nil, fmt.Errorf("blocked outbound to private address %s", tcp.IP)
				}
			}
			return c, nil
		}
	} else {
		tr.DialContext = dialer.DialContext
	}
	return &Engine{
		client: &http.Client{
			Timeout:   o.Timeout,
			Transport: tr,
			// Re-validate the per-plugin domain allowlist on EVERY redirect hop and
			// cap hops. Without this, an allowlisted host could 302 to an arbitrary
			// PUBLIC attacker host (the dial-time IP guard only blocks PRIVATE
			// targets), turning any plugin into a confused-deputy exfil channel for
			// the injected credential. Domains are carried per-request via context.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= maxRedirects {
					return fmt.Errorf("too many redirects (%d)", len(via))
				}
				domains, _ := req.Context().Value(domainsCtxKey{}).([]string)
				if len(domains) == 0 {
					return fmt.Errorf("redirect blocked: no domain allowlist in context")
				}
				if err := shortcut.CheckURLHost(req.URL.String(), domains); err != nil {
					return fmt.Errorf("redirect blocked: %w", err)
				}
				return nil
			},
		},
		maxBodyBytes: o.MaxBodyBytes,
	}
}

// domainsCtxKey carries the per-request domain allowlist into CheckRedirect.
type domainsCtxKey struct{}

// maxRedirects caps redirect hops (each hop is re-validated against the allowlist).
const maxRedirects = 3

// Run interprets fs with the given inputs (FormItem values) and auth (user
// credentials keyed by Auth.ID), returning the mapped output (Result property
// key → value). It validates fs first (defense in depth), enforces the domain
// allowlist on every request, and never writes host data.
func (e *Engine) Run(ctx context.Context, fs shortcut.FieldShortcut, inputs map[string]any, auth map[string]string) (map[string]any, error) {
	if err := fs.Validate(); err != nil {
		return nil, fmt.Errorf("invalid shortcut: %w", err)
	}
	if inputs == nil {
		inputs = map[string]any{}
	}

	var res any
	var err error
	switch {
	case len(fs.Steps) > 0:
		res, err = e.runSteps(ctx, fs, inputs)
	case strings.TrimSpace(fs.Execute.URL) != "":
		res, err = e.runSingle(ctx, fs, inputs, auth)
	default:
		// compute-only: no fetch; result exprs reference inputs / templates only.
		res = nil
	}
	if err != nil {
		return nil, err
	}
	return e.mapResult(fs, inputs, res)
}

// runSteps executes an ordered pipeline; each step's parsed JSON binds to its id
// for later steps, and the last step's response is returned as `res`.
func (e *Engine) runSteps(ctx context.Context, fs shortcut.FieldShortcut, inputs map[string]any) (any, error) {
	stepRes := map[string]any{}
	var last any
	for i, s := range fs.Steps {
		u, err := render(s.URL, inputs, stepRes)
		if err != nil {
			return nil, fmt.Errorf("step %q url: %w", s.ID, err)
		}
		if err := shortcut.CheckURLHost(u, fs.Domains); err != nil {
			return nil, fmt.Errorf("step %q: %w", s.ID, err)
		}
		headers, err := renderMap(s.Headers, inputs, stepRes)
		if err != nil {
			return nil, fmt.Errorf("step %q headers: %w", s.ID, err)
		}
		body, ctype, err := buildBody(s.Method, s.Body, s.BodyJSON, inputs, stepRes)
		if err != nil {
			return nil, fmt.Errorf("step %q body: %w", s.ID, err)
		}
		if ctype != "" {
			headers["Content-Type"] = ctype
		}
		resp, err := e.fetch(ctx, fs.Domains, s.Method, u, headers, body)
		if err != nil {
			return nil, fmt.Errorf("step %q (%d): %w", s.ID, i, err)
		}
		stepRes[s.ID] = resp
		last = resp
	}
	return last, nil
}

// runSingle performs the single Execute request, injecting the user's auth
// credential per Auth.Type.
func (e *Engine) runSingle(ctx context.Context, fs shortcut.FieldShortcut, inputs map[string]any, auth map[string]string) (any, error) {
	u, err := render(fs.Execute.URL, inputs, nil)
	if err != nil {
		return nil, fmt.Errorf("url: %w", err)
	}
	headers, err := renderMap(fs.Execute.Headers, inputs, nil)
	if err != nil {
		return nil, fmt.Errorf("headers: %w", err)
	}
	if fs.Auth != nil {
		cred := auth[fs.Auth.ID]
		u, err = applyAuth(fs.Auth, cred, u, headers)
		if err != nil {
			return nil, err
		}
	}
	if err := shortcut.CheckURLHost(u, fs.Domains); err != nil {
		return nil, err
	}
	body, ctype, err := buildBody(fs.Execute.Method, fs.Execute.Body, fs.Execute.BodyJSON, inputs, nil)
	if err != nil {
		return nil, fmt.Errorf("body: %w", err)
	}
	if ctype != "" {
		headers["Content-Type"] = ctype
	}
	return e.fetch(ctx, fs.Domains, fs.Execute.Method, u, headers, body)
}

// applyAuth injects the user-supplied credential per the SDK auth type. Returns
// the (possibly query-augmented) URL.
func applyAuth(a *shortcut.Auth, cred, u string, headers map[string]string) (string, error) {
	switch a.Type {
	case "HeaderBearerToken":
		headers["Authorization"] = "Bearer " + cred
	case "QueryParamToken":
		sep := "?"
		if strings.Contains(u, "?") {
			sep = "&"
		}
		u += sep + url.QueryEscape(a.ParamName) + "=" + url.QueryEscape(cred)
	case "CustomHeaderToken":
		headers[a.ParamName] = cred
	case "Basic":
		headers["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(cred))
	default:
		return "", fmt.Errorf("unsupported auth type %q", a.Type)
	}
	return u, nil
}

// fetch performs one outbound request and returns its parsed JSON body. Only
// http/https are allowed; the response is size-capped and must be JSON. domains
// is the per-plugin host allowlist, carried in the request context so redirect
// hops (CheckRedirect) are re-validated against it.
func (e *Engine) fetch(ctx context.Context, domains []string, method, u string, headers map[string]string, body []byte) (any, error) {
	parsed, err := url.Parse(u)
	if err != nil {
		return nil, fmt.Errorf("bad url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("scheme %q not allowed", parsed.Scheme)
	}
	ctx = context.WithValue(ctx, domainsCtxKey{}, domains)
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, e.maxBodyBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("upstream %d: %s", resp.StatusCode, truncate(string(raw), 200))
	}
	var parsedJSON any
	if err := json.Unmarshal(raw, &parsedJSON); err != nil {
		return nil, fmt.Errorf("response not JSON: %w", err)
	}
	return parsedJSON, nil
}

// mapResult evaluates each result property (Template or Expr) over the inputs +
// final response, producing the output map. Hidden props (e.g. the _id group
// key) are included so the renderer/writeback gets the full object.
func (e *Engine) mapResult(fs shortcut.FieldShortcut, inputs map[string]any, res any) (map[string]any, error) {
	out := map[string]any{}
	for _, p := range fs.Result.Properties {
		var v any
		switch {
		case strings.TrimSpace(p.Template) != "":
			s, err := render(p.Template, inputs, nil)
			if err != nil {
				return nil, fmt.Errorf("result %q template: %w", p.Key, err)
			}
			v = s
		case strings.TrimSpace(p.Expr) != "":
			ev, err := evalExpr(p.Expr, inputs, res)
			if err != nil {
				return nil, fmt.Errorf("result %q expr: %w", p.Key, err)
			}
			v = ev
		}
		// Mirror the compiler: a Url column is written as a Base URL-cell { text, link }
		// (shortcut.renderPropValue), so both runtime paths produce a clickable link cell
		// rather than a plain string on the self-hosted path.
		if p.Type == "Url" && v != nil {
			v = map[string]any{"text": v, "link": v}
		}
		out[p.Key] = v
	}
	return out, nil
}

// --- template rendering -------------------------------------------------------

var placeholderRe = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_.]*)\}`)

// render substitutes {key} (input) and {stepID.path} (prior step response) into
// a template string, stringifying values. Matches the compiler's placeholder
// resolution (inputs + prior steps only).
func render(tpl string, inputs map[string]any, stepRes map[string]any) (string, error) {
	var firstErr error
	out := placeholderRe.ReplaceAllStringFunc(tpl, func(m string) string {
		name := m[1 : len(m)-1]
		if i := strings.IndexByte(name, '.'); i >= 0 {
			head, rest := name[:i], name[i+1:]
			if stepRes != nil {
				if v, ok := stepRes[head]; ok {
					return toStr(getPath(v, rest))
				}
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("placeholder {%s} references unknown prior step", name)
			}
			return m
		}
		if v, ok := inputs[name]; ok {
			return toStr(v)
		}
		if firstErr == nil {
			firstErr = fmt.Errorf("placeholder {%s} references unknown input", name)
		}
		return m
	})
	return out, firstErr
}

func renderMap(in map[string]string, inputs map[string]any, stepRes map[string]any) (map[string]string, error) {
	out := make(map[string]string, len(in))
	for k, v := range in {
		r, err := render(v, inputs, stepRes)
		if err != nil {
			return nil, err
		}
		out[k] = r
	}
	return out, nil
}

// buildBody renders a flat Body map or a structured BodyJSON into a JSON request
// body. Returns (nil, "", nil) for body-less methods.
func buildBody(method string, flat map[string]string, bodyJSON json.RawMessage, inputs map[string]any, stepRes map[string]any) ([]byte, string, error) {
	if method != "POST" && method != "PUT" && method != "PATCH" {
		return nil, "", nil
	}
	if len(flat) > 0 {
		obj := make(map[string]any, len(flat))
		for k, v := range flat {
			r, err := render(v, inputs, stepRes)
			if err != nil {
				return nil, "", err
			}
			obj[k] = r
		}
		b, err := json.Marshal(obj)
		return b, "application/json", err
	}
	if len(bodyJSON) > 0 {
		var doc any
		if err := json.Unmarshal(bodyJSON, &doc); err != nil {
			return nil, "", fmt.Errorf("bodyJson: %w", err)
		}
		rendered, err := renderJSON(doc, inputs, stepRes)
		if err != nil {
			return nil, "", err
		}
		b, err := json.Marshal(rendered)
		return b, "application/json", err
	}
	return nil, "", nil
}

// renderJSON walks a decoded JSON document, substituting placeholders in every
// string leaf (objects/arrays recursed; non-strings untouched).
func renderJSON(v any, inputs map[string]any, stepRes map[string]any) (any, error) {
	switch x := v.(type) {
	case string:
		return render(x, inputs, stepRes)
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, vv := range x {
			r, err := renderJSON(vv, inputs, stepRes)
			if err != nil {
				return nil, err
			}
			out[k] = r
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, vv := range x {
			r, err := renderJSON(vv, inputs, stepRes)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	default:
		return v, nil
	}
}

// proxyAddrs returns the host:port of every egress proxy configured in the
// environment (HTTP_PROXY/HTTPS_PROXY/ALL_PROXY). The SSRF dial guard allows
// connecting to these (they are the egress control point) while still blocking
// every other private target. host:port is matched exactly, so a proxy on
// 127.0.0.1:1082 does NOT whitelist a different loopback port.
func proxyAddrs() map[string]bool {
	out := map[string]bool{}
	for _, k := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy"} {
		v := os.Getenv(k)
		if v == "" {
			continue
		}
		if !strings.Contains(v, "://") {
			v = "http://" + v
		}
		if u, err := url.Parse(v); err == nil && u.Host != "" {
			host := u.Host
			if u.Port() == "" {
				switch u.Scheme {
				case "https":
					host += ":443"
				default:
					host += ":80"
				}
			}
			out[host] = true
		}
	}
	return out
}

// blockedCIDRs are reserved/special ranges Go's net helpers do NOT classify as
// private but that must never be reachable from the SSRF surface — most notably
// the carrier-grade NAT range (RFC 6598 100.64.0.0/10), which on Alibaba Cloud
// hosts the instance metadata service at 100.100.100.200 (stealing the runner's
// RAM-role credentials would escalate one malicious plugin to cloud-account
// compromise). Cloud metadata IPs are listed explicitly as defense in depth.
var blockedCIDRs = func() []*net.IPNet {
	cidrs := []string{
		"100.64.0.0/10",      // RFC 6598 CGNAT (Alibaba metadata lives here)
		"192.0.0.0/24",       // RFC 6890 IETF protocol assignments
		"192.0.2.0/24",       // TEST-NET-1
		"198.18.0.0/15",      // benchmarking
		"198.51.100.0/24",    // TEST-NET-2
		"203.0.113.0/24",     // TEST-NET-3
		"240.0.0.0/4",        // reserved (incl. 255.255.255.255 broadcast)
		"100.100.100.200/32", // Alibaba Cloud metadata
		"169.254.169.254/32", // AWS/GCP/Azure metadata (also covered by link-local)
	}
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		if _, n, err := net.ParseCIDR(c); err == nil {
			out = append(out, n)
		}
	}
	return out
}()

// isPrivateIP reports loopback / link-local / private / unspecified / reserved
// addresses the SSRF guard refuses. It normalizes IPv4-mapped IPv6 so an
// attacker cannot smuggle a blocked v4 address in ::ffff: form.
func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsPrivate() || ip.IsUnspecified() {
		return true
	}
	cand := ip
	if v4 := ip.To4(); v4 != nil {
		cand = v4 // covers ::ffff:a.b.c.d mapped addresses
	}
	for _, n := range blockedCIDRs {
		if n.Contains(ip) || n.Contains(cand) {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
