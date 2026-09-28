package pagerduty

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/nicolasb114/pd-status-wall/internal/model"
	"github.com/nicolasb114/pd-status-wall/internal/store"
)

// Poller runs a ticker that periodically refreshes the store's snapshot from
// the PagerDuty API. On any failure it leaves the previous snapshot in
// place: the display page must never go blank.
type Poller struct {
	store *store.Store
}

func NewPoller(s *store.Store) *Poller {
	return &Poller{store: s}
}

// Run blocks until ctx is cancelled, polling immediately and then on the
// configured interval (re-read from the store on every tick, so an admin
// changing the poll interval takes effect on the next cycle).
func (p *Poller) Run(ctx context.Context) {
	for {
		p.pollAndStore(ctx)

		interval := p.store.Get().PollIntervalSeconds
		if interval <= 0 {
			interval = 45
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(interval) * time.Second):
		}
	}
}

func (p *Poller) pollAndStore(ctx context.Context) {
	cfg := p.store.Get()
	prev := p.store.Snapshot()

	if cfg.PDAPIKey == "" || cfg.StatusPageID == "" {
		p.store.SetSnapshot(model.Snapshot{
			GeneratedAt:    time.Now(),
			Overall:        model.StatusOperational,
			OverallMessage: "Not configured yet - open /admin to connect PagerDuty.",
			LastPollAt:     time.Now(),
			LastPollOK:     false,
			LastError:      "no API key or status page configured",
		})
		return
	}

	snap, err := p.poll(ctx, cfg)
	if err != nil {
		log.Printf("poll failed, keeping last known-good snapshot: %v", err)
		prev.LastError = err.Error()
		prev.LastPollAt = time.Now()
		prev.LastPollOK = false
		p.store.SetSnapshot(prev)
		return
	}

	snap.LastPollAt = time.Now()
	snap.LastPollOK = true
	snap.LastError = ""
	p.store.SetSnapshot(snap)
}

func (p *Poller) poll(ctx context.Context, cfg model.Config) (model.Snapshot, error) {
	client := NewClient(cfg.PDAPIKey, Region(cfg.PDRegion))

	pageServices, err := client.ListStatusPageServices(ctx, cfg.StatusPageID)
	if err != nil {
		return model.Snapshot{}, fmt.Errorf("list status page services: %w", err)
	}
	pageServices = applyOrder(pageServices, cfg.ServiceOrder)

	services, err := buildServices(ctx, client, pageServices, cfg.ShowSubServices)
	if err != nil {
		return model.Snapshot{}, err
	}
	top := applyGroups(services, cfg.ServiceGroups)

	overall := model.WorstStatus(top)
	var degraded []string
	for _, n := range top {
		if n.Status != model.StatusOperational {
			degraded = append(degraded, n.Name)
		}
	}
	message := "Everything is running smoothly"
	if len(degraded) > 0 {
		message = "Impacted: " + strings.Join(degraded, ", ")
	}

	return model.Snapshot{
		GeneratedAt:    time.Now(),
		Overall:        overall,
		OverallMessage: message,
		StatusPageName: cfg.StatusPageName,
		Services:       top,
	}, nil
}

// applyOrder reorders pageServices per the admin-configured business
// service order. Services not present in the configured order keep their
// natural (API-returned) relative order at the end.
func applyOrder(pageServices []StatusPageService, order []string) []StatusPageService {
	if len(order) == 0 {
		return pageServices
	}
	pos := make(map[string]int, len(order))
	for i, id := range order {
		pos[id] = i
	}
	sorted := make([]StatusPageService, len(pageServices))
	copy(sorted, pageServices)
	sort.SliceStable(sorted, func(i, j int) bool {
		pi, oki := pos[sorted[i].BusinessService.ID]
		pj, okj := pos[sorted[j].BusinessService.ID]
		if oki && okj {
			return pi < pj
		}
		if oki != okj {
			return oki // configured ones sort before unconfigured ones
		}
		return false
	})
	return sorted
}

// buildServices turns the status page's service entries into display
// nodes: one node per service shown on the page, in page order, each with
// its own status.
//
// It deliberately does NOT walk PagerDuty's service-dependency graph. That
// graph is an internal impact-calculation model, not a display hierarchy -
// walking it produces deep, heavily duplicated trees (the same service
// appearing under several parents and again as its own entry) that look
// nothing like the status page it is meant to mirror. When
// showSubServices is enabled, exactly one level of each service's direct
// supporting services is added, and no further.
func buildServices(ctx context.Context, client *Client, pageServices []StatusPageService, showSubServices bool) ([]model.Node, error) {
	ids := make([]string, 0, len(pageServices))
	for _, s := range pageServices {
		ids = append(ids, s.BusinessService.ID)
	}

	impacted, err := client.BusinessServiceImpacts(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("business service impacts: %w", err)
	}

	nodes := make([]model.Node, 0, len(pageServices))
	for _, s := range pageServices {
		id := s.BusinessService.ID
		node := model.Node{
			ID:     id,
			Name:   s.Name,
			Kind:   "business_service",
			Status: model.StatusOperational,
		}
		if impacted[id] {
			node.Status = model.StatusImpacted
		}

		if showSubServices {
			children, err := supportingServices(ctx, client, id, impacted)
			if err != nil {
				return nil, err
			}
			node.Children = children
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

// supportingServices returns one level of a business service's direct
// supporting services - no recursion into their own dependencies.
func supportingServices(ctx context.Context, client *Client, businessServiceID string, impacted map[string]bool) ([]model.Node, error) {
	rels, err := client.BusinessServiceDependencies(ctx, businessServiceID)
	if err != nil {
		return nil, fmt.Errorf("dependencies for business service %s: %w", businessServiceID, err)
	}

	var children []model.Node
	var nestedBusinessIDs []string
	for _, r := range rels {
		if r.SupportingService.ID == "" {
			continue
		}
		if normalizeKind(r.SupportingService.Type) == "business_service" {
			nestedBusinessIDs = append(nestedBusinessIDs, r.SupportingService.ID)
		}
	}

	// Nested business services need their computed impact status, which
	// comes from a single batched call rather than one request each.
	nestedImpacts := map[string]bool{}
	if len(nestedBusinessIDs) > 0 {
		nestedImpacts, err = client.BusinessServiceImpacts(ctx, nestedBusinessIDs)
		if err != nil {
			return nil, fmt.Errorf("impacts for supporting business services of %s: %w", businessServiceID, err)
		}
	}

	for _, r := range rels {
		ref := r.SupportingService
		if ref.ID == "" {
			continue
		}
		if normalizeKind(ref.Type) == "business_service" {
			bs, err := client.GetBusinessService(ctx, ref.ID)
			if err != nil {
				return nil, fmt.Errorf("get business service %s: %w", ref.ID, err)
			}
			status := model.StatusOperational
			if nestedImpacts[ref.ID] {
				status = model.StatusImpacted
			}
			children = append(children, model.Node{
				ID:     ref.ID,
				Name:   bs.Name,
				Kind:   "business_service",
				Status: status,
			})
			continue
		}

		svc, err := client.GetService(ctx, ref.ID)
		if err != nil {
			return nil, fmt.Errorf("get service %s: %w", ref.ID, err)
		}
		children = append(children, model.Node{
			ID:     ref.ID,
			Name:   svc.Name,
			Kind:   "service",
			Status: mapServiceStatus(svc.Status),
		})
	}
	return children, nil
}

// applyGroups folds the flat service list into the admin-defined groups.
// A group is emitted at the position of its first member, so the existing
// service ordering also controls where groups land; services belonging to
// no group stay where they are. A group's status is the worst status
// among its members.
func applyGroups(services []model.Node, groups []model.ServiceGroup) []model.Node {
	if len(groups) == 0 {
		return services
	}

	byID := make(map[string]model.Node, len(services))
	for _, n := range services {
		byID[n.ID] = n
	}

	groupOf := make(map[string]int, len(services)) // service ID -> group index
	for gi, g := range groups {
		for _, id := range g.Services {
			if _, ok := byID[id]; ok {
				groupOf[id] = gi
			}
		}
	}

	emitted := make(map[int]bool, len(groups))
	out := make([]model.Node, 0, len(services))
	for _, n := range services {
		gi, grouped := groupOf[n.ID]
		if !grouped {
			out = append(out, n)
			continue
		}
		if emitted[gi] {
			continue // already rendered as part of its group
		}
		emitted[gi] = true

		g := groups[gi]
		members := make([]model.Node, 0, len(g.Services))
		for _, id := range g.Services {
			if member, ok := byID[id]; ok {
				members = append(members, member)
			}
		}
		out = append(out, model.Node{
			ID:       "group-" + g.Name,
			Name:     g.Name,
			Kind:     "group",
			Status:   model.WorstStatus(members),
			Children: members,
		})
	}
	return out
}

func normalizeKind(apiType string) string {
	if strings.HasPrefix(apiType, "business_service") {
		return "business_service"
	}
	return "service"
}

func mapServiceStatus(pdStatus string) model.Status {
	switch pdStatus {
	case "active":
		return model.StatusOperational
	case "warning":
		return model.StatusWarning
	case "critical":
		return model.StatusCritical
	case "maintenance":
		return model.StatusMaintenance
	case "disabled":
		return model.StatusDisabled
	default:
		return model.StatusOperational
	}
}
