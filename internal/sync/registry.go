package sync

import (
	"fmt"
	"sync"

	"github.com/agent-sync/agent-sync/internal/db"
	"github.com/agent-sync/agent-sync/internal/sync/providers"
	"github.com/agent-sync/agent-sync/pkg/types"
)

type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
	db        *db.DB
}

func NewRegistry(database *db.DB) *Registry {
	return &Registry{
		providers: make(map[string]Provider),
		db:        database,
	}
}

func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[string(p.Type())] = p
}

func (r *Registry) Get(pt types.ProviderType) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[string(pt)]
	return p, ok
}

func (r *Registry) List() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var list []Provider
	for _, p := range r.providers {
		list = append(list, p)
	}
	return list
}

func (r *Registry) DetectAll() []Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var detected []Provider
	for _, p := range r.providers {
		ok, err := p.Detect()
		if err == nil && ok {
			detected = append(detected, p)
		}
	}
	return detected
}

func (r *Registry) InitDefaultProviders(cfg providers.Config) {
	providersList := []struct {
		t  types.ProviderType
		fn func(cfg providers.Config) (Provider, error)
	}{
		{types.ProviderClaudeCode, func(cfg providers.Config) (Provider, error) {
			return providers.NewClaudeCodeProvider(cfg, r.db)
		}},
		{types.ProviderOpenCode, func(cfg providers.Config) (Provider, error) {
			return providers.NewOpenCodeProvider(cfg, r.db)
		}},
		{types.ProviderCursor, func(cfg providers.Config) (Provider, error) {
			return providers.NewCursorProvider(cfg, r.db)
		}},
	}

	for _, p := range providersList {
		provider, err := p.fn(cfg)
		if err != nil {
			fmt.Printf("  ! %s: %v\n", p.t, err)
			continue
		}
		r.Register(provider)
	}
}
