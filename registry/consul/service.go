package consul

import (
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/juicesix/go-upstream/registry"
	"github.com/juicesix/logging"
)

type endpointSlice []registry.Endpoint

func (s endpointSlice) Less(i, j int) bool {
	return s[i].ID < s[j].ID
}

func (s endpointSlice) Len() int {
	return len(s)
}

func (s endpointSlice) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

// watchService 监控 consul 健康检查并在每次更改时创建新配置
func watchService(logger *logging.Logger, client *api.Client, service string, status []string, dc string, config chan<- []*registry.Cluster) {
	var lastIndex uint64
	var oldCheckers []*api.HealthCheck
	var lastConfig []*registry.Cluster

	for {
		q := &api.QueryOptions{RequireConsistent: true, WaitIndex: lastIndex}
		q.Datacenter = dc
		services, meta, err := client.Health().Service(service, "", false, q)
		if err != nil {
			logger.Warnf("consul:Error fetching health state. %v", err)
			time.Sleep(time.Second)
			continue
		}
		checks := make([]*api.HealthCheck, 0)
		for _, srv := range services {
			checks = append(checks, srv.Checks...)
		}

		lastIndex = meta.LastIndex
		passCheckers := passingServices(logger, checks, status)
		logger.Infof("consul: Service %s Datacenter %s Health changed to #%d", service, dc, meta.LastIndex)
		if checkCheckersEqual(oldCheckers, passCheckers) {
			logger.Infof("consul: Service %s Datacenter %s Health changed to #%d, but passing list not changed", service, dc, meta.LastIndex)
			continue
		}
		oldCheckers = passCheckers
		newConfig := servicesConfig(logger, client, passCheckers, dc)
		if checkClusterChanged(newConfig, lastConfig) {
			config <- newConfig
		} else {
			logger.Infof("consul: Service %s Datacenter %s Health changed to #%d, but server list not changed", service, dc, meta.LastIndex)
		}
		lastConfig = newConfig
	}
}

// servicesConfig 确定哪些服务实例通过了健康检查，然后找到具有正确前缀标签的服务实例来构建配置
func servicesConfig(logger *logging.Logger, client *api.Client, checks []*api.HealthCheck, dc string) []*registry.Cluster {
	// 将服务名称映射到健康检查正常的服务通过列表
	m := map[string]map[string]bool{}
	for _, check := range checks {
		name, id := check.ServiceName, check.ServiceID

		if _, ok := m[name]; !ok {
			m[name] = map[string]bool{}
		}
		m[name][id] = true
	}

	var clusters []*registry.Cluster
	for name, passing := range m {
		cluster := serviceConfig(logger, client, name, passing, dc)
		clusters = append(clusters, cluster)
	}

	return clusters
}

// serviceConfig 为单个服务的所有良好实例构造配置
func serviceConfig(logger *logging.Logger, client *api.Client, name string, passing map[string]bool, dc string) (cluster *registry.Cluster) {
	cluster = &registry.Cluster{
		Name:      name,
		Endpoints: []registry.Endpoint{},
	}
	if name == "" || len(passing) == 0 {
		return
	}

	q := &api.QueryOptions{RequireConsistent: true}
	q.Datacenter = dc
	svcs, _, err := client.Catalog().Service(name, "", q)
	if err != nil {
		logger.Warnf("consul:Error getting catalog service %s. %v", name, err)
		return
	}

	defer cluster.AddEnvTag()

	for _, svc := range svcs {
		// 检查实例是否在通过健康检查的实例列表中
		if _, ok := passing[svc.ServiceID]; !ok {
			continue
		}

		// 获取所有没有标签前缀的标签
		var svctags []string
		svctags = append(svctags, svc.ServiceTags...)
		svctags = append(svctags, "dc="+svc.Datacenter)
		sort.Strings(svctags)
		addr := svc.ServiceAddress
		if addr == "" {
			addr = svc.Address
		}
		e := registry.Endpoint{
			ID:   svc.ServiceID,
			Addr: addr,
			Port: svc.ServicePort,
			Tags: svctags,
		}
		cluster.Endpoints = append(cluster.Endpoints, e)
	}
	return
}

func checkCheckersEqual(old, new []*api.HealthCheck) bool {
	// 检查h1的所有元素是否都在h2中
	checkIn := func(h1, h2 []*api.HealthCheck) bool {
		for _, h := range h1 {
			found := false
			for _, j := range h2 {
				if j.Node == h.Node && j.CheckID == h.CheckID && j.Name == h.Name && j.Status == h.Status && j.ServiceID == h.ServiceID &&
					j.ServiceName == h.ServiceName && reflect.DeepEqual(j.ServiceTags, h.ServiceTags) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}
	return checkIn(old, new) && checkIn(new, old)
}

func checkClusterChanged(new, last []*registry.Cluster) bool {
	if len(new) != len(last) {
		return true
	}
	var checkCluster = func(c1, c2 *registry.Cluster) bool {
		sort.Sort(endpointSlice(c1.Endpoints))
		sort.Sort(endpointSlice(c2.Endpoints))
		return !reflect.DeepEqual(c1.Endpoints, c2.Endpoints)
	}
	for i := 0; i < len(new); i++ {
		if checkCluster(new[i], last[i]) {
			return true
		}
	}
	return false
}

// watchServices 监控 consul 健康检查，并在所有数据中心的每次更改时创建新配置
func watchServices(logger *logging.Logger, client *api.Client, service string, status []string, dc string, config chan<- []*registry.Cluster) {
	datacenters := strings.Split(strings.TrimSpace(dc), ",")
	eventChan := make([]chan []*registry.Cluster, len(datacenters))
	for i := range eventChan {
		eventChan[i] = make(chan []*registry.Cluster)
	}
	lastConfig := make([][]*registry.Cluster, len(datacenters))
	var lastResult []*registry.Cluster

	for i, dc := range datacenters {
		go watchService(logger, client, service, status, dc, eventChan[i])
	}
	cases := make([]reflect.SelectCase, len(datacenters))
	for i, ch := range eventChan {
		cases[i] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(ch)}
	}
	for {
		chosen, value, ok := reflect.Select(cases)
		if !ok {
			cases[chosen].Chan = reflect.ValueOf(nil)
			continue
		}
		lastConfig[chosen] = value.Interface().([]*registry.Cluster)
		result := []*registry.Cluster{
			&registry.Cluster{
				Name:      service,
				Endpoints: nil,
			},
		}
		for i := range lastConfig {
			if len(lastConfig[i]) != 0 && len(lastConfig[i][0].Endpoints) != 0 {
				result[0].Endpoints = append(result[0].Endpoints, lastConfig[i][0].Endpoints...)
			}
		}
		if checkClusterChanged(result, lastResult) {
			config <- result
		} else {
			logger.Infof("consul Service %s Datacenter %s Health changed but server list not changed", service, dc)
		}
		lastResult = result
	}
}
