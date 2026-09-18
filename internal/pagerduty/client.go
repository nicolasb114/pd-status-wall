// Package pagerduty is a small, outbound-only client for the parts of the
// PagerDuty REST API this app needs: status pages, business services,
// service dependencies, and technical services. It also implements the
// background poller that turns those calls into a model.Snapshot.
package pagerduty

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Region selects which PagerDuty API host to call.
type Region string

const (
	RegionUS Region = "us"
	RegionEU Region = "eu"
)

func (r Region) baseURL() string {
	if r == RegionEU {
		return "https://api.eu.pagerduty.com"
	}
	return "https://api.pagerduty.com"
}

// Client is a minimal, read-only PagerDuty REST API client.
type Client struct {
	apiKey     string
	region     Region
	httpClient *http.Client
}

func NewClient(apiKey string, region Region) *Client {
	return &Client{
		apiKey: apiKey,
		region: region,
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
	}
}

// apiError is returned when PagerDuty responds with a non-2xx status after
// rate-limit backoff has already been applied.
type apiError struct {
	Status int
	Body   string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("pagerduty api: unexpected status %d: %s", e.Status, e.Body)
}

// get performs an authenticated GET request against the PagerDuty API,
// honoring rate-limit response headers: on a 429 it waits until
// ratelimit-reset (capped at 60s) and retries once.
func (c *Client) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	fullURL := c.region.baseURL() + path
	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Token token="+c.apiKey)
		req.Header.Set("Accept", "application/vnd.pagerduty+json;version=2")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("request %s: %w", path, err)
		}

		if resp.StatusCode == http.StatusTooManyRequests && attempt == 0 {
			wait := rateLimitWait(resp.Header)
			resp.Body.Close()
			select {
			case <-time.After(wait):
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read response %s: %w", path, err)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, &apiError{Status: resp.StatusCode, Body: string(body)}
		}
		return body, nil
	}
	return nil, fmt.Errorf("request %s: exhausted retries after rate limit", path)
}

// rateLimitWait computes how long to back off after a 429, based on the
// ratelimit-reset header (unix seconds), capped to a sane maximum so a
// misbehaving header never stalls the poller for an unreasonable time.
func rateLimitWait(h http.Header) time.Duration {
	const maxWait = 60 * time.Second
	reset := h.Get("ratelimit-reset")
	if reset == "" {
		return 5 * time.Second
	}
	secs, err := strconv.ParseInt(reset, 10, 64)
	if err != nil {
		return 5 * time.Second
	}
	wait := time.Until(time.Unix(secs, 0))
	if wait <= 0 {
		return time.Second
	}
	if wait > maxWait {
		return maxWait
	}
	return wait
}

// --- Resource types (only the fields this app uses) ---

type StatusPage struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type reference struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type StatusPageService struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	BusinessService reference `json:"business_service"`
}

type BusinessService struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Service struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type Relationship struct {
	SupportingService reference `json:"supporting_service"`
	DependentService  reference `json:"dependent_service"`
}

// ListStatusPages lists every status page on the account, so the admin
// panel can offer a dropdown of which one to display.
func (c *Client) ListStatusPages(ctx context.Context) ([]StatusPage, error) {
	body, err := c.get(ctx, "/status_pages", nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		StatusPages []StatusPage `json:"status_pages"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode status pages: %w", err)
	}
	return out.StatusPages, nil
}

// ListStatusPageServices returns the services configured to display on a
// specific status page, each referencing the business service it wraps.
func (c *Client) ListStatusPageServices(ctx context.Context, statusPageID string) ([]StatusPageService, error) {
	body, err := c.get(ctx, "/status_pages/"+url.PathEscape(statusPageID)+"/services", nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Services []StatusPageService `json:"services"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode status page services: %w", err)
	}
	return out.Services, nil
}

// GetBusinessService fetches a single business service (used to resolve the
// display name of business services nested underneath another one, which
// aren't listed directly on the status page).
func (c *Client) GetBusinessService(ctx context.Context, id string) (BusinessService, error) {
	body, err := c.get(ctx, "/business_services/"+url.PathEscape(id), nil)
	if err != nil {
		return BusinessService{}, err
	}
	var out struct {
		BusinessService BusinessService `json:"business_service"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return BusinessService{}, fmt.Errorf("decode business service: %w", err)
	}
	return out.BusinessService, nil
}

// BusinessServiceImpacts returns, for each requested business service id,
// whether PagerDuty currently considers it impacted. This uses PagerDuty's
// own computed impact status (which already applies the account's global
// priority threshold), rather than re-implementing that logic here.
func (c *Client) BusinessServiceImpacts(ctx context.Context, ids []string) (map[string]bool, error) {
	result := make(map[string]bool, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	q := url.Values{}
	for _, id := range ids {
		q.Add("ids[]", id)
	}
	body, err := c.get(ctx, "/business_services/impacts", q)
	if err != nil {
		return nil, err
	}
	var out struct {
		Services []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"services"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode business service impacts: %w", err)
	}
	for _, s := range out.Services {
		result[s.ID] = s.Status == "impacted"
	}
	// Anything PagerDuty didn't return (e.g. never impacted, not in the
	// "most impacted" window) is assumed not impacted.
	for _, id := range ids {
		if _, ok := result[id]; !ok {
			result[id] = false
		}
	}
	return result, nil
}

// GetService fetches a single technical service, including its current
// active/warning/critical/maintenance/disabled status.
func (c *Client) GetService(ctx context.Context, id string) (Service, error) {
	body, err := c.get(ctx, "/services/"+url.PathEscape(id), nil)
	if err != nil {
		return Service{}, err
	}
	var out struct {
		Service Service `json:"service"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return Service{}, fmt.Errorf("decode service: %w", err)
	}
	return out.Service, nil
}

// BusinessServiceDependencies returns the immediate supporting services
// (technical or nested business services) for a business service.
func (c *Client) BusinessServiceDependencies(ctx context.Context, id string) ([]Relationship, error) {
	return c.dependencies(ctx, "/service_dependencies/business_services/"+url.PathEscape(id))
}

// TechnicalServiceDependencies returns the immediate supporting services for
// a technical service (e.g. "Events API" depending on "Events API (US)" and
// "Events API (EU)").
func (c *Client) TechnicalServiceDependencies(ctx context.Context, id string) ([]Relationship, error) {
	return c.dependencies(ctx, "/service_dependencies/technical_services/"+url.PathEscape(id))
}

func (c *Client) dependencies(ctx context.Context, path string) ([]Relationship, error) {
	body, err := c.get(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Relationships []Relationship `json:"relationships"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode dependencies: %w", err)
	}
	return out.Relationships, nil
}
