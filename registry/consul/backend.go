package consul

import (
	"errors"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/juicesix/go-upstream/config"
	"github.com/juicesix/go-upstream/registry"
	"github.com/juicesix/logging"
)

type be struct {
	c      *api.Client
	dc     string
	cfg    *config.Consul
	logger *logging.Logger
}

func NewBackend(cfg *config.Consul) (registry.Backend, error) {
	// create a reusable client
	c, err := api.NewClient(&api.Config{Address: cfg.Addr, Scheme: cfg.Scheme, Token: cfg.Token, WaitTime: 60 * time.Second})
	if err != nil {
		return nil, err
	}
	logger := cfg.Logger
	if logger == nil {
		logger = logging.New()
	}

	// ping the agent
	dc, err := datacenter(c)
	if err != nil {
		logger.Warnf("consul backend init error %s", err)
	}

	logger.Infof("consul:Connecting to %q in datacenter %q", cfg.Addr, dc)
	return &be{c: c, dc: dc, cfg: cfg, logger: logger}, nil
}

func datacenter(c *api.Client) (string, error) {
	self, err := c.Agent().Self()
	if err != nil {
		return "", err
	}

	cfg, ok := self["Config"]
	if !ok {
		return "", errors.New("consul: self.Config not found")
	}
	dc, ok := cfg["Datacenter"].(string)
	if !ok {
		return "", errors.New("consul: self.Datacenter not found")
	}
	return dc, nil
}

// Register 注册服务
func (b *be) Register(cfg *config.Register) error {
	tagsValue := <-cfg.TagsOverrideCh
	service, err := serviceRegistration(cfg, tagsValue)
	if err != nil {
		return err
	}
	cfg.DerigesterCh = register(b.logger, b.c, service, cfg.TagsOverrideCh)
	return nil
}

// Deregister 注销服务
func (b *be) Deregister(cfg *config.Register) error {
	cfg.DerigesterCh <- true // trigger deregistration
	<-cfg.DerigesterCh
	return nil
}

func (b *be) ReadManual(KVPath string) (value string, version uint64, err error) {
	return getKV(b.c, KVPath, 0)
}

func (b *be) WriteManual(KVPath, value string, version uint64) (ok bool, err error) {
	if ok, err = putKV(b.c, KVPath, value, 0); ok {
		return
	}

	return putKV(b.c, KVPath, value, version)
}

func (b *be) WatchServices(name string, status []string, dc string) chan []*registry.Cluster {
	b.logger.Infof("consul:Watching Services %q,status %v", name, status)

	svc := make(chan []*registry.Cluster)
	go watchServices(b.logger, b.c, name, status, dc, svc)
	return svc
}

func (b *be) WatchManual(KVPath string) chan string {
	test := make(chan string)
	return test
}

func (b *be) WatchPrefixManual(prefix string) chan map[string]string {
	test := make(chan map[string]string)
	return test
}
