package consul

import (
	"strings"

	"github.com/hashicorp/consul/api"
	"github.com/juicesix/logging"
)

// passServices 过滤出健康检查通过了健康检查的服务，并且服务实例本身和节点都没有处于维护模式
func passingServices(logger *logging.Logger, checks []*api.HealthCheck, status []string) []*api.HealthCheck {
	var p []*api.HealthCheck
	for _, svc := range checks {
		// 首先过滤掉非服务检查
		if svc.ServiceID == "" || svc.CheckID == "serfHealth" || svc.CheckID == "_node_maintenance" || strings.HasPrefix("_service_maintenance:", svc.CheckID) {
			continue
		}
		// 然后确保服务健康检查通过
		if !contains(status, svc.Status) {
			continue
		}

		// 然后检查代理是否还活着，并且节点和服务都没有处于维护模式
		for _, c := range checks {
			if c.CheckID == "serfHealth" && c.Node == svc.Node && c.Status == "critical" {
				logger.Infof("consul:Skipping service %q since agent on node %q is down :%s", svc.ServiceID, svc.Node, c.Output)
			}
			if c.CheckID == "_node_maintenance" && c.Node == svc.Node {
				logger.Infof("consul: Skipping service %q since node %q is in maintenance mode: %s", svc.ServiceID, svc.Node, c.Output)
				goto skip
			}
			if c.CheckID == "_service_maintenance:"+svc.ServiceID && c.Status == "critical" {
				logger.Infof("consul: Skipping service %q since it is in maintenance mode: %s", svc.ServiceID, c.Output)
				goto skip
			}
		}

		p = append(p, svc)

	skip:
	}
	return p
}

func contains(slice []string, item string) bool {
	for _, a := range slice {
		if a == item {
			return true
		}
	}
	return false
}
