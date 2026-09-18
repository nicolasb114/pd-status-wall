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

	topLevel, err := client.ListStatusPageServices(ctx, cfg.StatusPageID)
	if err != nil {
		return model.Snapshot{}, fmt.Errorf("list status page services: %w", err)
	}
	topLevel = applyOrder(topLevel, cfg.ServiceOrder)

	services, err := buildTree(ctx, client, topLevel)
	if err != nil {
		return model.Snapshot{}, err
	}

	overall := model.StatusOperational
	var impacted []string
	for _, n := range services {
		if n.Status == model.StatusImpacted {
			overall = model.StatusImpacted
			impacted = append(impacted, n.Name)
		}
	}
	message := "Everything is running smoothly"
	if overall == model.StatusImpacted {
		message = "Impacted: " + strings.Join(impacted, ", ")
	}

	return model.Snapshot{
		GeneratedAt:    time.Now(),
		Overall:        overall,
		OverallMessage: message,
		StatusPageName: cfg.StatusPageName,
		Services:       services,
	}, nil
}

// applyOrder reorders topLevel per the admin-configured business service
// order. Services not present in the configured order keep their natural
// (API-returned) relative order at the end.
func applyOrder(topLevel []StatusPageService, order []string) []StatusPageService {
	if len(order) == 0 {
		return topLevel
	}
	pos := make(map[string]int, len(order))
	for i, id := range order {
		pos[id] = i
	}
	sorted := make([]StatusPageService, len(topLevel))
	copy(sorted, topLevel)
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

// buildTree walks the dependency graph starting from the business services
// shown on the status page, producing a generic tree of nodes. Any kind of
// service (business or technical) can have children of either kind - the
// same collapsible structure is reused regardless of what the nesting
// represents (business service -> supporting services, or a single service
// broken into regional sub-components).
func buildTree(ctx context.Context, client *Client, topLevel []StatusPageService) ([]model.Node, error) {
	bsIDs := map[string]bool{}
	bsName := map[string]string{}
	svcIDs := map[string]bool{}
	deps := map[string][]Relationship{} // keyed by "kind:id"

	depKey := func(kind, id string) string { return kind + ":" + id }

	var walk func(kind, id string) error
	walk = func(kind, id string) error {
		key := depKey(kind, id)
		if _, done := deps[key]; done {
			return nil
		}
		var rels []Relationship
		var err error
		if kind == "business_service" {
			rels, err = client.BusinessServiceDependencies(ctx, id)
		} else {
			rels, err = client.TechnicalServiceDependencies(ctx, id)
		}
		if err != nil {
			return fmt.Errorf("dependencies for %s %s: %w", kind, id, err)
		}
		deps[key] = rels
		for _, r := range rels {
			cid, ckind := r.SupportingService.ID, r.SupportingService.Type
			if cid == "" {
				continue
			}
			if ckind == "business_service" {
				bsIDs[cid] = true
			} else {
				svcIDs[cid] = true
			}
			if err := walk(ckind, cid); err != nil {
				return err
			}
		}
		return nil
	}

	for _, s := range topLevel {
		bsIDs[s.BusinessService.ID] = true
		if s.Name != "" {
			bsName[s.BusinessService.ID] = s.Name
		}
		if err := walk("business_service", s.BusinessService.ID); err != nil {
			return nil, err
		}
	}

	// Resolve display names for any business service we don't already have
	// a name for (nested ones aren't listed directly on the status page).
	for id := range bsIDs {
		if _, ok := bsName[id]; ok {
			continue
		}
		bs, err := client.GetBusinessService(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("get business service %s: %w", id, err)
		}
		bsName[id] = bs.Name
	}

	allBSIDs := make([]string, 0, len(bsIDs))
	for id := range bsIDs {
		allBSIDs = append(allBSIDs, id)
	}
	impacted, err := client.BusinessServiceImpacts(ctx, allBSIDs)
	if err != nil {
		return nil, fmt.Errorf("business service impacts: %w", err)
	}

	svc := map[string]Service{}
	for id := range svcIDs {
		s, err := client.GetService(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("get service %s: %w", id, err)
		}
		svc[id] = s
	}

	var build func(kind, id, fallbackName string) model.Node
	build = func(kind, id, fallbackName string) model.Node {
		node := model.Node{ID: id, Kind: kind}
		if kind == "business_service" {
			node.Name = bsName[id]
			if node.Name == "" {
				node.Name = fallbackName
			}
			if impacted[id] {
				node.Status = model.StatusImpacted
			} else {
				node.Status = model.StatusOperational
			}
		} else {
			s := svc[id]
			node.Name = s.Name
			if node.Name == "" {
				node.Name = fallbackName
			}
			node.Status = mapServiceStatus(s.Status)
		}
		for _, r := range deps[depKey(kind, id)] {
			cid, ckind := r.SupportingService.ID, r.SupportingService.Type
			if cid == "" {
				continue
			}
			childName := ""
			if ckind == "business_service" {
				childName = bsName[cid]
			} else {
				childName = svc[cid].Name
			}
			node.Children = append(node.Children, build(ckind, cid, childName))
		}
		return node
	}

	result := make([]model.Node, 0, len(topLevel))
	for _, s := range topLevel {
		result = append(result, build("business_service", s.BusinessService.ID, s.Name))
	}
	return result, nil
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
