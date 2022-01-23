package consul

import (
	"time"

	"GoUpstream/config"
	"GoUpstream/logging"
	"GoUpstream/registry"
	"github.com/hashicorp/consul/api"
)

type be struct {
	c      *api.Client
	dc     string
	cfg    *config.Consul
	logger *logging.Logger
}

func NewBackend(cfg *config.Consul) (registry.Backend, error) {
	// create a reusable client
	api.NewClient(&api.Config{Address: cfg.Addr, Scheme: cfg.Scheme, Token: cfg.Token, WaitTime: 60 * time.Second})
}
