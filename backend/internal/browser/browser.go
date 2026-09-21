// Package browser drives a real Chrome instance for the adapters.
//
// Why a browser instead of an HTTP client: these platforms sit behind bot
// management (Blinkit is on Cloudflare) that rejects synthetic clients even
// when the TLS fingerprint is spoofed. A real browser engine passes without a
// fight. We still get clean structured data by intercepting the JSON/XHR
// responses the page itself makes, rather than scraping the rendered DOM.
package browser

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

const UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"

// Pool hands out browser contexts. When RemoteURL is set it attaches to an
// already-running Chrome (the headless-shell container in docker compose);
// otherwise it launches a local Chrome, which is the nicer story when you are
// debugging on your laptop with Headless=false.
type Pool struct {
	RemoteURL string
	Headless  bool

	mu        sync.Mutex
	allocCtx  context.Context
	allocStop context.CancelFunc
}

func NewPool(remoteURL string, headless bool) *Pool {
	return &Pool{RemoteURL: remoteURL, Headless: headless}
}

func (p *Pool) allocator() (context.Context, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.allocCtx != nil {
		return p.allocCtx, nil
	}

	if p.RemoteURL != "" {
		target, err := resolveHostToIP(p.RemoteURL)
		if err != nil {
			return nil, err
		}
		ctx, cancel := chromedp.NewRemoteAllocator(context.Background(), target)
		p.allocCtx, p.allocStop = ctx, cancel
		return ctx, nil
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:], stealthFlags(p.Headless)...)
	ctx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	p.allocCtx, p.allocStop = ctx, cancel
	return ctx, nil
}

// resolveHostToIP rewrites a DevTools URL's hostname to its resolved IP.
//
// Chrome refuses DevTools requests whose Host header is neither an IP nor
// localhost, so connecting to a compose service by name ("http://chrome:9222")
// is rejected outright. Resolving the name ourselves keeps the readable service
// name in configuration while satisfying that check.
func resolveHostToIP(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse chrome url %q: %w", raw, err)
	}
	host, port := u.Hostname(), u.Port()
	if net.ParseIP(host) != nil {
		return raw, nil
	}

	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return "", fmt.Errorf("resolve chrome host %q: %w", host, err)
	}
	u.Host = ips[0].String()
	if port != "" {
		u.Host = net.JoinHostPort(ips[0].String(), port)
	}
	return u.String(), nil
}

// stealthFlags configure Chrome to look like an ordinary desktop browser.
//
// Chrome's *new* headless mode is what matters most here: Swiggy blocks the
// legacy headless build outright ("your request looks automated"), but accepts
// --headless=new. The rest suppress the obvious automation tells.
func stealthFlags(headless bool) []chromedp.ExecAllocatorOption {
	mode := "new"
	if !headless {
		mode = "false"
	}
	return []chromedp.ExecAllocatorOption{
		chromedp.Flag("headless", mode),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("excludeSwitches", "enable-automation"),
		chromedp.Flag("disable-features", "IsolateOrigins,site-per-process"),
		chromedp.WindowSize(1440, 900),
		chromedp.UserAgent(UserAgent),
	}
}

func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.allocStop != nil {
		p.allocStop()
		p.allocStop, p.allocCtx = nil, nil
	}
}

// Session is one isolated browser tab for a single adapter call.
type Session struct {
	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	captured map[string][][]byte
	patterns []string
	// pending maps an in-flight request we care about to the pattern it matched,
	// so its body can be read once loading finishes.
	pending map[network.RequestID]string

	// host labels dumped payload files, so a dump directory from a four-way
	// fan-out is still readable.
	host string
}

// NewSession opens a tab, grants geolocation for origin, and pins the device
// location to lat/lon so the platform resolves the dark store we actually want.
func (p *Pool) NewSession(ctx context.Context, origin string, lat, lon float64) (*Session, error) {
	alloc, err := p.allocator()
	if err != nil {
		return nil, err
	}

	tabCtx, cancelTab := chromedp.NewContext(alloc)
	cancel := cancelTab
	// Inherit the caller's deadline so a slow platform cannot hang a search.
	if dl, ok := ctx.Deadline(); ok {
		var cancelDeadline context.CancelFunc
		tabCtx, cancelDeadline = context.WithDeadline(tabCtx, dl)
		cancel = func() { cancelDeadline(); cancelTab() }
	}

	s := &Session{
		ctx: tabCtx, cancel: cancel,
		captured: map[string][][]byte{},
		pending:  map[network.RequestID]string{},
		host:     hostOf(origin),
	}

	if err := chromedp.Run(tabCtx,
		network.Enable(),
		// Applied per-session because the remote headless-shell gets no exec
		// flags from us, and its own UA advertises "HeadlessChrome".
		emulation.SetUserAgentOverride(UserAgent).
			WithAcceptLanguage("en-GB,en-US;q=0.9,en;q=0.8").
			WithPlatform("MacIntel"),
		browser.SetPermission(
			&browser.PermissionDescriptor{Name: "geolocation"},
			browser.PermissionSettingGranted,
		).WithOrigin(origin),
		emulation.SetGeolocationOverride().
			WithLatitude(lat).WithLongitude(lon).WithAccuracy(50),
		// navigator.webdriver is the cheapest automation tell to remove.
		chromedp.ActionFunc(func(c context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(
				`Object.defineProperty(navigator,'webdriver',{get:()=>undefined});`).Do(c)
			return err
		}),
	); err != nil {
		cancel()
		return nil, fmt.Errorf("open session: %w", err)
	}

	s.listen()
	return s, nil
}

func (s *Session) Close() { s.cancel() }

// Capture registers a URL substring whose JSON response bodies should be kept.
// Register before navigating.
func (s *Session) Capture(pattern string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.patterns = append(s.patterns, pattern)
}

// listen records response bodies for the registered patterns.
//
// Bodies are read on Network.loadingFinished rather than on responseReceived.
// A response's body is not readable when its headers land, so reading on
// responseReceived means guessing at a delay, and guessing costs bodies:
// Chrome evicts them from its network buffer, and a large response that is
// still being fetched when the guess expires is simply lost. loadingFinished is
// the event that says the body is complete, so reading there is both correct
// and as early as possible.
func (s *Session) listen() {
	chromedp.ListenTarget(s.ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *network.EventResponseReceived:
			if match := s.match(e.Response.URL); match != "" {
				s.mu.Lock()
				s.pending[e.RequestID] = match
				s.mu.Unlock()
			}

		case *network.EventLoadingFinished:
			s.mu.Lock()
			match, ok := s.pending[e.RequestID]
			delete(s.pending, e.RequestID)
			s.mu.Unlock()
			if !ok {
				return
			}

			// Off the event goroutine: this makes a CDP call of its own, and
			// blocking here would stall every later event.
			go s.fetchBody(e.RequestID, match)
		}
	})
}

func (s *Session) match(url string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.patterns {
		if strings.Contains(url, p) {
			return p
		}
	}
	return ""
}

func (s *Session) fetchBody(id network.RequestID, match string) {
	c := chromedp.FromContext(s.ctx)
	if c == nil || c.Target == nil {
		return
	}
	body, err := network.GetResponseBody(id).Do(cdp.WithExecutor(s.ctx, c.Target))
	if err != nil || len(body) == 0 {
		return
	}
	s.mu.Lock()
	s.captured[match] = append(s.captured[match], body)
	s.mu.Unlock()

	s.dump(match, body)
}

// dump writes one captured payload to KH_DUMP_DIR when that is set.
//
// Every adapter failure in this codebase eventually reduces to "the JSON moved":
// a widget renamed, a field nested one level deeper, a response that never
// arrived at all. Without the raw bodies that is guesswork, because by the time
// an adapter reports "no products parsed" the browser is closed and the
// evidence is gone. This is the cheapest possible hook, since fetchBody is the
// one place every capture passes through, and it costs nothing when the
// variable is unset.
//
//	KH_DUMP_DIR=/tmp/kh make probe P=minutes Q="maggi noodles"
//	jq . /tmp/kh/www.flipkart.com-api-4-page-fetch-001.json
func (s *Session) dump(match string, body []byte) {
	dir := os.Getenv("KH_DUMP_DIR")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	name := fmt.Sprintf("%s-%s-%03d.json", s.host, slug(match), dumpSeq.Add(1))
	_ = os.WriteFile(filepath.Join(dir, name), body, 0o644)
}

// dumpSeq orders the files across a parallel fan-out, where four sessions write
// into the same directory at once.
var dumpSeq atomic.Int64

// slug turns a URL pattern into something safe for a filename.
func slug(s string) string {
	s = strings.Trim(s, "/")
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.':
			return r
		default:
			return '-'
		}
	}, s)
}

// hostOf reduces an origin to its hostname, falling back to a slug of whatever
// was passed when it does not parse as a URL.
func hostOf(origin string) string {
	if u, err := url.Parse(origin); err == nil && u.Host != "" {
		return u.Host
	}
	return slug(origin)
}

// Navigate loads url and waits settle for in-flight XHRs to complete.
func (s *Session) Navigate(url string, settle time.Duration) error {
	return chromedp.Run(s.ctx, chromedp.Navigate(url), chromedp.Sleep(settle))
}

// NavigateAndWait loads url and returns as soon as a body for pattern has been
// captured, rather than sleeping for a fixed settle window. Falls back to
// whatever arrived by maxWait.
func (s *Session) NavigateAndWait(url, pattern string, maxWait time.Duration) error {
	if err := chromedp.Run(s.ctx, chromedp.Navigate(url)); err != nil {
		return err
	}
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		if len(s.Payloads(pattern)) > 0 {
			// Let sibling requests (pagination, a second widget page) land too.
			time.Sleep(700 * time.Millisecond)
			return nil
		}
		select {
		case <-s.ctx.Done():
			return s.ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return nil
}

// Payloads returns every captured body for a registered pattern.
func (s *Session) Payloads(pattern string) [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([][]byte, len(s.captured[pattern]))
	copy(out, s.captured[pattern])
	return out
}

// ClickXPath clicks the first node matching an XPath expression, waiting up to
// timeout for it to appear.
//
// Real clicks, not dispatched events: Flipkart's picker is react-native-web,
// whose responder system ignores synthetic MouseEvents but reacts normally to
// input that arrives through the CDP input domain.
func (s *Session) ClickXPath(xpath string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(s.ctx, timeout)
	defer cancel()
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(xpath, chromedp.BySearch),
		chromedp.Click(xpath, chromedp.BySearch),
	); err != nil {
		return fmt.Errorf("click %q: %w", xpath, err)
	}
	return nil
}

// HasXPath reports whether a node matching xpath is present right now.
func (s *Session) HasXPath(xpath string) bool {
	var n []*cdp.Node
	err := chromedp.Run(s.ctx, chromedp.Nodes(xpath, &n, chromedp.BySearch, chromedp.AtLeast(0)))
	return err == nil && len(n) > 0
}

// WaitPayload blocks until a body for pattern is captured, or maxWait elapses.
// Reports whether anything arrived.
func (s *Session) WaitPayload(pattern string, maxWait time.Duration) bool {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		if len(s.Payloads(pattern)) > 0 {
			return true
		}
		select {
		case <-s.ctx.Done():
			return false
		case <-time.After(250 * time.Millisecond):
		}
	}
	return len(s.Payloads(pattern)) > 0
}

// ScrollToBottom jumps to the end of the page, which is what triggers the next
// batch on an infinite-scroll results page.
//
// It scrolls the window and, separately, the tallest overflowing element on the
// page. The second part is what actually does the work on react-native-web
// apps, where the document itself never scrolls and the list lives inside its
// own scroll view. That element is found by geometry rather than by selector
// because these apps ship hashed class names that change on every deploy.
func (s *Session) ScrollToBottom() error {
	const js = `(() => {
		window.scrollTo(0, document.body.scrollHeight);
		let best = null, bestOverflow = 200;
		for (const el of document.querySelectorAll('div')) {
			const overflow = el.scrollHeight - el.clientHeight;
			if (overflow > bestOverflow) { best = el; bestOverflow = overflow; }
		}
		if (!best) return false;
		best.scrollTop = best.scrollHeight;
		return true;
	})()`
	var scrolled bool
	return chromedp.Run(s.ctx, chromedp.Evaluate(js, &scrolled))
}

// Eval runs JS in the page and decodes the result into out.
func (s *Session) Eval(js string, out interface{}) error {
	return chromedp.Run(s.ctx, chromedp.Evaluate(js, out))
}
