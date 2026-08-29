package linkedin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// The client speaks flagship-web's RSC endpoints with a discover-then-replay
// strategy:
//
//  1. POST /flagship-web/in/<vanity>/ with an SDUI NavigateToScreen action.
//     The response is the page shell plus the top card and -- crucially -- it
//     enumerates every lazily-loaded card the browser would request on scroll,
//     as AsyncComponentRequest nodes carrying both the newComponentId and the
//     exact requestedArguments payload to send.
//  2. Replay each against POST /flagship-web/rsc-action/actions/component.
//
// Nothing about the card list is hardcoded, so when LinkedIn adds, renames or
// re-partitions a card -- the sections below Activity are currently split
// across profileCardsBelowActivityPart1WithoutExp .. Part7 -- the client picks
// the change up on the next request instead of silently returning fewer
// fields.

const (
	// BaseURL is LinkedIn's origin.
	BaseURL = "https://www.linkedin.com"

	navPath       = "/flagship-web/in/%s/"
	componentPath = "/flagship-web/rsc-action/actions/component"
	profileScreen = "com.linkedin.sdui.flagshipnav.profile.Profile"
	cardPrefix    = "com.linkedin.sdui.generated.profile.dsl.impl."
	clientVersion = "0.2.7003"

	// ActivityCard is the recent-posts card. Several MB, so opt-in only.
	ActivityCard = "profileCardsActivity"
)

// WantedCards are the cards worth fetching for a profile dump. Everything else
// the shell advertises is recommendations or ads (pymk, browsemap, product) and
// costs a request for data that is not part of the profile.
var WantedCards = []string{
	"profileCardsAboveActivity",  // About, Featured, Services, Highlights
	"profileCardsExperienceOnly", // Experience
	"profileCardsBelowActivityPart1WithoutExp",
	"profileCardsBelowActivityPart2",
	"profileCardsBelowActivityPart3",
	"profileCardsBelowActivityPart4",
	"profileCardsBelowActivityPart5",
	"profileCardsBelowActivityPart6",
	"profileCardsBelowActivityPart7",
}

// Card is one lazily-loaded profile card, ready to replay.
type Card struct {
	ComponentID       string
	RequestedArgument *Object
}

// ShortName is the card id without its package prefix.
func (c Card) ShortName() string {
	if index := strings.LastIndex(c.ComponentID, "."); index >= 0 {
		return c.ComponentID[index+1:]
	}
	return c.ComponentID
}

// Options configure a Client.
type Options struct {
	LiAt        string
	JSessionID  string
	Timeout     time.Duration
	Concurrency int
	BaseURL     string
	HTTPClient  *http.Client
	Logger      *slog.Logger
}

// Client is an authenticated session against flagship-web.
//
// It reads only what the backing account can already see; there is no
// privilege escalation here, and an expired session yields nothing.
type Client struct {
	baseURL     string
	jsessionID  string
	liAt        string
	concurrency int
	http        *http.Client
	log         *slog.Logger
}

// New builds a Client. Both cookies are required.
func New(opts Options) (*Client, error) {
	jsessionID := strings.Trim(strings.TrimSpace(opts.JSessionID), `"`)
	if opts.LiAt == "" || jsessionID == "" {
		return nil, errSessionExpired("LI_AT and LI_JSESSIONID must both be configured", "")
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = 4
	}
	base := opts.BaseURL
	if base == "" {
		base = BaseURL
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: timeout,
			// Never follow redirects: a bounce to /authwall or /login means
			// the session is unusable and must surface as such.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		baseURL: strings.TrimRight(base, "/"), jsessionID: jsessionID, liAt: opts.LiAt,
		concurrency: concurrency, http: httpClient, log: logger,
	}, nil
}

// FetchResult is what one profile fetch produced.
//
// Failed carries the cards LinkedIn refused. They used to be logged and
// dropped, which left the response indistinguishable from a member who simply
// has no experience section: HTTP 200, empty arrays, and a meta reporting
// nothing wrong. A caller cannot tell absent data from a broken fetch unless
// the failures survive.
type FetchResult struct {
	Cards  map[string]string
	Failed []string
}

// FetchProfile returns {cardName: flightText}, including "shell", plus the
// names of any cards that could not be fetched.
func (c *Client) FetchProfile(ctx context.Context, vanity string, includeActivity bool) (FetchResult, error) {
	shell, err := c.Navigate(ctx, vanity)
	if err != nil {
		return FetchResult{}, err
	}
	cards, err := DiscoverCards(shell, includeActivity)
	if err != nil {
		return FetchResult{}, err
	}
	names := make([]string, 0, len(cards))
	for _, card := range cards {
		names = append(names, card.ShortName())
	}
	c.log.Info("discovered cards", "vanity", vanity, "count", len(cards), "cards", names)

	results := map[string]string{"shell": shell}
	if len(cards) == 0 {
		return FetchResult{Cards: results}, nil
	}
	var failed []string

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		fatal    error
		gate     = make(chan struct{}, c.concurrency)
		fetchCtx = ctx
	)
	for _, card := range cards {
		wg.Add(1)
		go func(card Card) {
			defer wg.Done()
			gate <- struct{}{}
			defer func() { <-gate }()

			body, err := c.FetchCard(fetchCtx, vanity, card)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				var domain *Error
				if ok := asError(err, &domain); ok && domain.Code == "session_expired" && fatal == nil {
					fatal = domain
					return
				}
				// One dud card must not sink the whole profile; the parser
				// reports which sections were unavailable.
				c.log.Warn("card failed", "card", card.ShortName(), "err", err)
				failed = append(failed, card.ShortName())
				return
			}
			results[card.ShortName()] = body
		}(card)
	}
	wg.Wait()
	if fatal != nil {
		return FetchResult{}, fatal
	}
	sort.Strings(failed)
	return FetchResult{Cards: results, Failed: failed}, nil
}

// Navigate fetches the page shell and top card.
func (c *Client) Navigate(ctx context.Context, vanity string) (string, error) {
	body, err := json.Marshal(navigateAction(vanity))
	if err != nil {
		return "", err
	}
	return c.post(ctx, fmt.Sprintf(navPath, url.PathEscape(vanity)), nil, body, vanity, "navigate")
}

// FetchCard replays one advertised card request.
func (c *Client) FetchCard(ctx context.Context, vanity string, card Card) (string, error) {
	arguments := map[string]any{
		"states":           []any{},
		"screenId":         profileScreen,
		"knownTemplateIds": []any{},
		"requestMetadata":  map[string]any{"$type": "proto.sdui.common.RequestMetadata"},
	}
	card.RequestedArgument.Each(func(key string, value any) {
		if key == "$type" || key == "requestedStateKeys" {
			return
		}
		arguments[key] = value
	})
	body, err := json.Marshal(map[string]any{"clientArguments": arguments})
	if err != nil {
		return "", err
	}
	query := url.Values{"componentId": {card.ComponentID}, "sduiid": {card.ComponentID}}
	return c.post(ctx, componentPath, query, body, vanity, card.ShortName())
}

func (c *Client) post(ctx context.Context, path string, query url.Values, body []byte, vanity, what string) (string, error) {
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	c.setHeaders(req, vanity)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", errUpstream(fmt.Sprintf("upstream request failed (%s)", what), err.Error())
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return "", errSessionExpired(
			"LinkedIn rejected the session cookie -- refresh LI_AT / LI_JSESSIONID",
			fmt.Sprintf("%s: HTTP %d", what, resp.StatusCode))
	case resp.StatusCode == http.StatusTooManyRequests:
		return "", errUpstreamRateLimited(fmt.Sprintf("%s: HTTP 429", what))
	case resp.StatusCode == http.StatusNotFound:
		return "", errNotFound(vanity)
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return "", errSessionExpired("LinkedIn redirected the request (auth wall)",
			fmt.Sprintf("%s: -> %s", what, resp.Header.Get("Location")))
	case resp.StatusCode >= 400:
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 400))
		return "", errUpstream(fmt.Sprintf("upstream returned HTTP %d (%s)", resp.StatusCode, what), string(snippet))
	}

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", errUpstream(fmt.Sprintf("reading upstream body failed (%s)", what), err.Error())
	}
	return string(payload), nil
}

func (c *Client) setHeaders(req *http.Request, vanity string) {
	track, _ := json.Marshal(map[string]any{
		"clientVersion": clientVersion, "mpVersion": clientVersion, "osName": "web",
		"timezoneOffset": 0, "timezone": "UTC", "deviceFormFactor": "DESKTOP",
		"mpName": "web", "displayDensity": 2, "displayWidth": 2560, "displayHeight": 1440,
	})
	header := req.Header
	header.Set("accept", "*/*")
	header.Set("accept-language", "en-US,en;q=0.9")
	header.Set("content-type", "application/json")
	// LinkedIn requires the csrf header to equal the JSESSIONID cookie.
	header.Set("csrf-token", c.jsessionID)
	header.Set("origin", BaseURL)
	header.Set("referer", BaseURL+"/in/"+vanity+"/")
	header.Set("x-li-rsc-stream", "true")
	header.Set("x-li-application-version", clientVersion)
	header.Set("x-li-track", string(track))
	header.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) "+
		"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36")
	// Accept-Encoding is deliberately unset so net/http negotiates gzip and
	// decompresses transparently. Asking for brotli would need a third-party
	// decoder for no benefit.
	header.Set("cookie", fmt.Sprintf(`li_at=%s; JSESSIONID="%s"`, c.liAt, c.jsessionID))
}

// DiscoverCards reads the lazily-loaded card list out of a shell response.
func DiscoverCards(shell string, includeActivity bool) ([]Card, error) {
	doc, err := ParseDocument(shell)
	if err != nil {
		return nil, ErrUnparseable("navigation response was not a Flight stream", err.Error())
	}

	order := make(map[string]int, len(WantedCards)+1)
	for i, name := range WantedCards {
		order[name] = i
	}
	if includeActivity {
		order[ActivityCard] = len(order)
	}

	found := make(map[string]Card)
	var descend func(node any, depth int)
	descend = func(node any, depth int) {
		if depth > maxDepth {
			return
		}
		switch value := node.(type) {
		case *Object:
			if value.Str("$type") == "proto.sdui.actions.core.AsyncComponentRequest" {
				collectCard(value, order, found)
			}
			value.Each(func(_ string, child any) { descend(child, depth+1) })
		case []any:
			for _, child := range value {
				descend(child, depth+1)
			}
		}
	}
	for _, row := range doc.Rows {
		if row.Kind == RowModel {
			descend(row.Value, 0)
		}
	}

	cards := make([]Card, 0, len(found))
	for _, card := range found {
		cards = append(cards, card)
	}
	// Preserve documented order so partitioned cards stay in page order.
	sort.Slice(cards, func(i, j int) bool {
		return order[cards[i].ShortName()] < order[cards[j].ShortName()]
	})
	return cards, nil
}

func collectCard(node *Object, order map[string]int, found map[string]Card) {
	componentID := node.Str("newComponentId")
	if componentID == "" || !strings.HasPrefix(componentID, cardPrefix) {
		return
	}
	short := componentID[strings.LastIndex(componentID, ".")+1:]
	if _, wanted := order[short]; !wanted {
		return
	}
	if _, exists := found[componentID]; exists {
		return
	}
	arguments, ok := node.Get("requestedArguments")
	if !ok {
		return
	}
	argumentsObj, ok := arguments.(*Object)
	if !ok {
		return
	}
	found[componentID] = Card{ComponentID: componentID, RequestedArgument: argumentsObj}
}

func navigateAction(vanity string) map[string]any {
	return map[string]any{
		"$type":             "proto.sdui.actions.core.NavigateToScreen",
		"screenId":          profileScreen,
		"pageKey":           "profile_view_base",
		"presentationStyle": "PresentationStyle_FULL_PAGE",
		"presentation": map[string]any{
			"$case":    "fullPage",
			"fullPage": map[string]any{"$type": "proto.sdui.actions.core.presentation.FullPagePresentation"},
		},
		"title":                            "",
		"url":                              "/in/" + vanity + "/",
		"inheritActor":                     false,
		"colorScheme":                      "ColorScheme_UNKNOWN",
		"disableScreenGutters":             false,
		"shouldHideMobileTopNavBar":        false,
		"shouldHideLoadingSpinner":         false,
		"replaceCurrentScreen":             false,
		"shouldHideMobileTopNavBarDivider": false,
		"clearBackStack":                   false,
		"screenTitle":                      []any{},
		"requestedArguments": map[string]any{
			"payload":          map[string]any{"vanityName": vanity, "isVanityNameResolved": false},
			"states":           []any{},
			"requestMetadata":  map[string]any{"$type": "proto.sdui.common.RequestMetadata"},
			"screenId":         "",
			"knownTemplateIds": []any{},
		},
	}
}
