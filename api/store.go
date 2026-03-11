package api

import (
	"fmt"
	"sync"
	"time"
)

// DomainStore is an in-memory store for domains.
type DomainStore struct {
	mu      sync.RWMutex
	domains map[string]Domain
}

func NewDomainStore() *DomainStore {
	return &DomainStore{domains: make(map[string]Domain)}
}

func (s *DomainStore) Add(name string) Domain {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := Domain{ID: fmt.Sprintf("d-%d", time.Now().UnixNano()), Name: name, CreatedAt: time.Now().UTC()}
	s.domains[d.ID] = d
	return d
}

func (s *DomainStore) List() []Domain {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Domain, 0, len(s.domains))
	for _, d := range s.domains {
		out = append(out, d)
	}
	return out
}

func (s *DomainStore) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.domains[id]
	delete(s.domains, id)
	return ok
}

// RouteStore is an in-memory store for routes.
type RouteStore struct {
	mu     sync.RWMutex
	routes map[string]Route
}

func NewRouteStore() *RouteStore {
	return &RouteStore{routes: make(map[string]Route)}
}

func (s *RouteStore) Add(r Route) Route {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ID = fmt.Sprintf("r-%d", time.Now().UnixNano())
	r.CreatedAt = time.Now().UTC()
	s.routes[r.ID] = r
	return r
}

func (s *RouteStore) List() []Route {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Route, 0, len(s.routes))
	for _, r := range s.routes {
		out = append(out, r)
	}
	return out
}

func (s *RouteStore) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.routes[id]
	delete(s.routes, id)
	return ok
}
