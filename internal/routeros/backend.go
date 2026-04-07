package routeros

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"sync"

	"github.com/gerolf-vent/mikrolb/internal/core"
	"github.com/gerolf-vent/mikrolb/internal/routeros/api"
	"github.com/gerolf-vent/mikrolb/internal/utils"
	"github.com/go-logr/logr"
	"github.com/tidwall/gjson"
)

var (
	ignoredIPFirewallFields = []string{
		".id",
		"bytes",
		"dynamic",
		"invalid",
		"log",
		"log-prefix",
		"packets",
		"vrf",
	}

	ignoredIPListFields = []string{
		".id",
		"dynamic",
		"creation-time",
	}

	ignoredIPAddressFields = []string{
		".id",
		"actual-interface",
		"deprecated",
		"dynamic",
		"eui-64",
		"from-pool",
		"global",
		"invalid",
		"network",
		"slave",
		"vrf",
	}
)

type backend struct {
	client *api.Client
	logger logr.Logger

	mu sync.Mutex
}

func NewBackend(client *api.Client, logger logr.Logger) *backend {
	return &backend{
		client: client,
		logger: logger,
	}
}

func (m *backend) Check() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	resp, err := m.client.Get("/system/package", nil)
	if err != nil {
		return "", fmt.Errorf("failed to check router packages: %w", err)
	}

	routerOSPackage := resp.Get("#(name==\"routeros\")")
	if !routerOSPackage.Exists() {
		return "", errors.New("RouterOS package not found")
	}

	routerOSVersion := routerOSPackage.Get("version").String()
	if routerOSVersion == "" {
		return "", errors.New("RouterOS version is empty")
	}

	return routerOSVersion, nil
}

func (m *backend) Setup() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, ipFamily := range []core.IPFamily{core.IPv4, core.IPv6} {
		_, err := m.ensureLBRejectRule(ipFamily)
		if err != nil {
			return fmt.Errorf("failed to ensure LB reject rule for family %s: %w", ipFamily, err)
		}

		_, err = m.ensureLBAcceptRule(ipFamily)
		if err != nil {
			return fmt.Errorf("failed to ensure LB accept rule for family %s: %w", ipFamily, err)
		}

		_, err = m.ensureLBMangleRule(ipFamily)
		if err != nil {
			return fmt.Errorf("failed to ensure LB mangle rule for family %s: %w", ipFamily, err)
		}

		_, err = m.ensureLBConnectionChainRule(ipFamily)
		if err != nil {
			return fmt.Errorf("failed to ensure LB connection chain rule for family %s: %w", ipFamily, err)
		}

		_, err = m.ensureSNATRule(ipFamily)
		if err != nil {
			return fmt.Errorf("failed to ensure SNAT rule for family %s: %w", ipFamily, err)
		}
	}

	return nil
}

func (m *backend) EnsureIPAdvertisement(ip netip.Addr, interfaceName string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if interfaceName == "" {
		var err error
		interfaceName, err = m.determineInterfaceForIP(ip)
		if err != nil {
			return "", fmt.Errorf("failed to automatically determine interface: %w", err)
		}
	}

	_, err := m.ensureIPAdvertisement(ip, interfaceName)
	if err != nil {
		return "", err
	}

	return interfaceName, nil
}

func (m *backend) DeleteIPAdvertisement(ip netip.Addr) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.deleteIPAvertisement(ip)
}

func (m *backend) EnsureService(svc *core.Service) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("ensuring service", "service", svc.Name)

	for _, family := range []core.IPFamily{core.IPv4, core.IPv6} {
		if svc.HasLBIP(family) {
			_, err := m.ensureLBServicePortDNATRules(family, svc)
			if err != nil {
				return fmt.Errorf("failed to ensure LB service port DNAT rules for family %s: %w", family, err)
			}

			_, err = m.ensureLBServicePortRules(family, svc)
			if err != nil {
				return fmt.Errorf("failed to ensure LB service port rules for family %s: %w", family, err)
			}

			_, err = m.ensureLBServiceRule(family, svc)
			if err != nil {
				return fmt.Errorf("failed to ensure LB service rule for family %s: %w", family, err)
			}

			_, err = m.ensureLBIPsList(family, svc)
			if err != nil {
				return fmt.Errorf("failed to ensure LB IPs list for family %s: %w", family, err)
			}
		} else {
			err := m.cleanupLBIPsList(family, svc.UID)
			if err != nil {
				return fmt.Errorf("failed to cleanup LB IPs list for family %s: %w", family, err)
			}

			err = m.deleteLBServiceRules(family, svc.UID)
			if err != nil {
				return fmt.Errorf("failed to delete LB service for family %s: %w", family, err)
			}
		}

		if svc.HasSNATIP(family) {
			_, err := m.ensureSNATSrcIPsList(family, svc)
			if err != nil {
				return fmt.Errorf("failed to ensure SNAT service IPs list for family %s: %w", family, err)
			}

			_, err = m.ensureSNATServiceRule(family, svc)
			if err != nil {
				return fmt.Errorf("failed to ensure SNAT service rule for family %s: %w", family, err)
			}
		} else {
			err := m.deleteSNATServiceRule(family, svc.UID)
			if err != nil {
				return fmt.Errorf("failed to delete SNAT service rule for family %s: %w", family, err)
			}

			err = m.deleteSNATSrcIPsList(family, svc.UID)
			if err != nil {
				return fmt.Errorf("failed to delete SNAT service IPs list for family %s: %w", family, err)
			}
		}
	}

	return nil
}

func (m *backend) DeleteService(name, uid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("deleting service", "service", name)

	var errs []error
	for _, family := range []core.IPFamily{core.IPv4, core.IPv6} {
		err := m.cleanupLBIPsList(family, uid)
		if err != nil {
			errs = append(errs, err)
		}

		err = m.deleteLBServiceRules(family, uid)
		if err != nil {
			errs = append(errs, err)
		}

		err = m.deleteSNATServiceRule(family, uid)
		if err != nil {
			errs = append(errs, err)
		}

		err = m.deleteSNATSrcIPsList(family, uid)
		if err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (m *backend) getLBIPsListName() string {
	return "mikrolb-lb-ips"
}

func (m *backend) getLBConnectionChainName() string {
	return "mikrolb-dnat"
}

func (m *backend) getLBServiceChainName(serviceUID string) string {
	return "mikrolb-svc-" + utils.GenerateUniqueHash(8, serviceUID)
}

func (m *backend) getLBServicePortChainName(serviceUID string, port core.EndpointPort) string {
	return m.getLBServiceChainName(serviceUID) + fmt.Sprintf("-port-%d-%s", port.Port, strings.ToLower(port.Protocol))
}

func (m *backend) getSNATChainName() string {
	return "mikrolb-snat"
}

func (m *backend) getSNATServiceIPsListName(serviceUID string) string {
	return "mikrolb-src-ips-" + utils.GenerateUniqueHash(8, serviceUID)
}

func (m *backend) ensureIPAdvertisement(ip netip.Addr, interfaceName string) ([]gjson.Result, error) {
	mask := "/32"
	family := "ip"
	if ip.Is6() {
		mask = "/128"
		family = "ipv6"
	}

	req := api.Request{
		"interface": interfaceName,
		"address":   ip.String() + mask,
		"comment":   "mikrolb: IP advertisement",
		"disabled":  false,
	}

	// IPv6 addresses need some additional parameters
	if ip.Is6() {
		req["advertise"] = false
		req["auto-link-local"] = true
		req["eui-64"] = false
		req["no-dad"] = false
	}

	resp, err := m.client.Sync(
		fmt.Sprintf("/%s/address", family),
		[]api.Request{req},
		api.Query{
			"address": ip.String() + mask,
		},
		nil, // No custom filter
		nil, // There should only be one entry per IP, so no need for a custom id function
		ignoredIPAddressFields,
	)

	return resp, err
}

func (m *backend) ensureLBMangleRule(family core.IPFamily) (gjson.Result, error) {
	resp, err := m.client.Sync(
		fmt.Sprintf("/%s/firewall/mangle", getAPIIPFamily(family)),
		[]api.Request{
			{
				"chain":               "prerouting",
				"action":              "mark-connection",
				"new-connection-mark": "mikrolb-lb-connection",
				"comment":             "mikrolb: mark LB connections",
				"disabled":            false,
				"log":                 false, // Fix: For patch to work
			},
		},
		api.Query{
			"chain":               "prerouting",
			"action":              "mark-connection",
			"new-connection-mark": "mikrolb-lb-connection",
		},
		nil, // No custom filter
		nil, // There should only be one entry per IP, so no need for a custom id function
		ignoredIPFirewallFields,
	)
	if err != nil {
		return gjson.Result{}, err
	}

	return resp[0], nil
}

func (m *backend) ensureLBAcceptRule(family core.IPFamily) (gjson.Result, error) {
	resp, err := m.client.Sync(
		fmt.Sprintf("/%s/firewall/filter", getAPIIPFamily(family)),
		[]api.Request{
			{
				"chain":           "forward",
				"action":          "accept",
				"connection-mark": "mikrolb-lb-connection",
				"comment":         "mikrolb: accept LB connections",
				"disabled":        false,
				"log":             false, // Fix: For patch to work
			},
		},
		api.Query{
			"chain":           "forward",
			"action":          "accept",
			"connection-mark": "mikrolb-lb-connection",
		},
		nil, // No custom filter
		nil, // There should only be one entry per IP, so no need for a custom id function
		ignoredIPFirewallFields,
	)
	if err != nil {
		return gjson.Result{}, err
	}

	return resp[0], nil
}

func (m *backend) ensureLBRejectRule(family core.IPFamily) (gjson.Result, error) {
	var icmpProtocol string
	switch family {
	case core.IPv4:
		icmpProtocol = "icmp"
	case core.IPv6:
		icmpProtocol = "icmpv6"
	default:
		return gjson.Result{}, fmt.Errorf("unsupported ip family: %s", family)
	}

	lbIPsListName := m.getLBIPsListName()

	resp, err := m.client.Sync(
		fmt.Sprintf("/%s/firewall/filter", getAPIIPFamily(family)),
		[]api.Request{
			{
				"chain":            "input",
				"action":           "reject",
				"reject-with":      "icmp-admin-prohibited",
				"protocol":         "!" + icmpProtocol,
				"dst-address-list": lbIPsListName,
				"comment":          "mikrolb: reject unmatched LB ports",
				"disabled":         false,
				"log":              false, // Fix: For patch to work
			},
		},
		api.Query{
			"chain":            "input",
			"action":           "reject",
			"dst-address-list": lbIPsListName,
		},
		nil, // No custom filter
		nil, // No id function, as we only expect one rule
		ignoredIPFirewallFields,
	)
	if err != nil {
		return gjson.Result{}, err
	}

	return resp[0], nil
}

func (m *backend) ensureLBIPsList(family core.IPFamily, svc *core.Service) ([]gjson.Result, error) {
	var mask string
	switch family {
	case core.IPv4:
		// No mask
	case core.IPv6:
		mask = "/128"
	default:
		return nil, fmt.Errorf("unsupported ip family: %s", family)
	}

	lbIPsListName := m.getLBIPsListName()

	reqs := make([]api.Request, 0, len(svc.LBIPs))
	for _, ip := range svc.LBIPs {
		if core.GetIPFamily(ip) != family {
			continue
		}

		reqs = append(reqs, api.Request{
			"list":     lbIPsListName,
			"address":  ip.String() + mask,
			"comment":  fmt.Sprintf("mikrolb: service %s (%s)", svc.Name, svc.UID),
			"disabled": false,
		})
	}

	resp, err := m.client.Sync(
		fmt.Sprintf("/%s/firewall/address-list", getAPIIPFamily(family)),
		reqs,
		api.Query{
			"list": lbIPsListName,
		},
		// Custom filter to only sync entries that match the services LB IPs or the
		// service uid in the comment, to avoid deleting entries from other services.
		func(r gjson.Result) bool {
			var matchedIP, matchedComment bool

			commentStr := r.Get("comment").String()
			matchedComment = strings.HasPrefix(commentStr, "mikrolb:") && strings.Contains(commentStr, svc.UID)

			existingIPStr := strings.Split(r.Get("address").String(), "/")[0]
			existingIP, err := netip.ParseAddr(existingIPStr)
			if err != nil {
				matchedIP = false // Exclude invalid addresses from deletion
			} else {
				matchedIP = slices.Contains(svc.LBIPs, existingIP)
			}

			return matchedComment || matchedIP
		},
		// Use address field as the unique identifier
		func(r gjson.Result) string {
			return r.Get("address").String()
		},
		ignoredIPListFields,
	)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (m *backend) ensureLBConnectionChainRule(family core.IPFamily) (gjson.Result, error) {
	lbConnectionChainName := m.getLBConnectionChainName()
	lbIPsListName := m.getLBIPsListName()

	resp, err := m.client.Sync(
		fmt.Sprintf("/%s/firewall/nat", getAPIIPFamily(family)),
		[]api.Request{
			{
				"chain":            "dstnat",
				"action":           "jump",
				"jump-target":      lbConnectionChainName,
				"dst-address-list": lbIPsListName,
				"comment":          "mikrolb: LB connections",
				"disabled":         false,
				"log":              false, // Fix: For patch to work
			},
		},
		api.Query{
			"chain":       "dstnat",
			"action":      "jump",
			"jump-target": lbConnectionChainName,
		},
		nil, // No custom filter
		nil, // No id function, as we only expect one rule
		ignoredIPFirewallFields,
	)
	if err != nil {
		return gjson.Result{}, err
	}

	return resp[0], nil
}

func (m *backend) ensureLBServiceRule(family core.IPFamily, service *core.Service) ([]gjson.Result, error) {
	var mask string
	switch family {
	case core.IPv4:
		// Fix: RouterOS removes a /32 from ipv4 addresses, so we must not add a mask here
	case core.IPv6:
		mask = "/128"
	default:
		return nil, fmt.Errorf("unsupported family: %s", family)
	}

	lbConnectionChainName := m.getLBConnectionChainName()
	lbServiceChainName := m.getLBServiceChainName(service.UID)

	var reqs []api.Request
	for _, ip := range service.LBIPs {
		if core.GetIPFamily(ip) != family {
			continue
		}

		reqs = append(reqs, api.Request{
			"chain":       lbConnectionChainName,
			"action":      "jump",
			"jump-target": lbServiceChainName,
			"dst-address": ip.String() + mask,
			"comment":     fmt.Sprintf("mikrolb: LB for service %s (%s)", service.Name, service.UID),
			"disabled":    false,
			"log":         false, // Fix: For patch to work
		})
	}

	resp, err := m.client.Sync(
		fmt.Sprintf("/%s/firewall/nat", getAPIIPFamily(family)),
		reqs,
		api.Query{
			"chain":       lbConnectionChainName,
			"action":      "jump",
			"jump-target": lbServiceChainName,
		},
		nil, // No custom filter
		// Use dst-address field as the unique identifier
		func(r gjson.Result) string {
			return r.Get("dst-address").String()
		},
		ignoredIPFirewallFields,
	)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (m *backend) ensureLBServicePortRules(family core.IPFamily, svc *core.Service) ([]gjson.Result, error) {
	lbServiceChainName := m.getLBServiceChainName(svc.UID)

	reqsByPort := make(map[string]api.Request)
	reqsCount := 0
	reqsChainNames := make(map[string]struct{})
	for _, ep := range svc.Endpoints {
		for _, port := range ep.Ports {
			if port.Protocol != "TCP" && port.Protocol != "UDP" {
				continue
			}

			reqsKey := fmt.Sprintf("%d/%s", port.Port, port.Protocol)
			if _, exists := reqsByPort[reqsKey]; exists {
				continue
			}

			lbServicePortChainName := m.getLBServicePortChainName(svc.UID, port)
			reqsByPort[reqsKey] = api.Request{
				"chain":       lbServiceChainName,
				"action":      "jump",
				"jump-target": lbServicePortChainName,
				"dst-port":    port.Port,
				"protocol":    strings.ToLower(port.Protocol),
				"comment":     fmt.Sprintf("mikrolb: LB for service %s (%s), port %d/%s", svc.Name, svc.UID, port.Port, port.Protocol),
				"disabled":    false,
				"log":         false, // Fix: For patch to work
			}
			reqsCount++
			reqsChainNames[lbServicePortChainName] = struct{}{}
		}
	}

	reqs := make([]api.Request, 0, reqsCount)
	for _, req := range reqsByPort {
		reqs = append(reqs, req)
	}

	staleChainNames := make(map[string]struct{})

	resp, err := m.client.Sync(
		fmt.Sprintf("/%s/firewall/nat", getAPIIPFamily(family)),
		reqs,
		api.Query{
			"chain": lbServiceChainName,
		},
		// Use the custom filter to identify stale port chains that need to be cleaned up.
		// There is no real filtering happening here.
		func(r gjson.Result) bool {
			chain := r.Get("jump-target").String()
			if _, exists := reqsChainNames[chain]; !exists {
				staleChainNames[chain] = struct{}{}
			}
			return true
		},
		// Use dst-port and protocol fields as the unique identifier
		func(r gjson.Result) string {
			return fmt.Sprintf("%s/%s", r.Get("dst-port").String(), r.Get("protocol").String())
		},
		ignoredIPFirewallFields,
	)
	if err != nil {
		return nil, err
	}

	// Clean up stale port chains
	for chainName := range staleChainNames {
		_, err := m.client.Sync(
			fmt.Sprintf("/%s/firewall/nat", getAPIIPFamily(family)),
			nil, // Delete all matching entries
			api.Query{
				"chain": chainName,
			},
			nil, // No custom filter, as we want to delete all rules in the chain
			nil, // No id function required for pruning
			nil, // Ignored fields don't matter
		)
		if err != nil {
			m.logger.Error(err, "failed to clean up stale LB service port chain", "chain", chainName)
		}
	}

	return resp, nil
}

func (m *backend) ensureLBServicePortDNATRules(family core.IPFamily, svc *core.Service) (map[string][]gjson.Result, error) {
	var addressField string
	var mask string
	switch family {
	case core.IPv4:
		addressField = "to-addresses"
		// No mask
	case core.IPv6:
		addressField = "to-address"
		mask = "/128"
	default:
		return nil, fmt.Errorf("unsupported family: %s", family)
	}

	// Group rules by service port chain
	reqsByChain := make(map[string][]api.Request)
	for _, ep := range svc.Endpoints {
		for _, port := range ep.Ports {
			if port.Protocol != "TCP" && port.Protocol != "UDP" {
				continue
			}

			lbServicePortChainName := m.getLBServicePortChainName(svc.UID, port)
			for _, ip := range ep.IPs {
				if core.GetIPFamily(ip) != family {
					continue
				}

				req := api.Request{
					"chain":      lbServicePortChainName,
					"action":     "dst-nat",
					addressField: ip.String() + mask,
					"to-ports":   port.TargetPort,
					"protocol":   strings.ToLower(port.Protocol),
					"comment":    fmt.Sprintf("mikrolb: LB for service %s (%s), port %d/%s -> %s", svc.Name, svc.UID, port.Port, port.Protocol, netip.AddrPortFrom(ip, port.TargetPort).String()),
					"disabled":   false,
					"log":        false, // Fix: For patch to work
				}

				reqsByChain[lbServicePortChainName] = append(reqsByChain[lbServicePortChainName], req)
			}
		}
	}

	// Add round-robin distribution
	for _, reqs := range reqsByChain {
		for i := range reqs {
			// The last rule doesn't need an nth parameter and will be the fallback/default
			if i+1 < len(reqs) {
				reqs[i]["nth"] = fmt.Sprintf("%d,%d", len(reqs), i+1)
			}
		}
	}

	respByChain := make(map[string][]gjson.Result)
	var errs []error
	for chain, reqs := range reqsByChain {
		resp, err := m.client.Sync(
			fmt.Sprintf("/%s/firewall/nat", getAPIIPFamily(family)),
			reqs,
			api.Query{
				"chain": chain,
			},
			nil,
			func(r gjson.Result) string {
				return fmt.Sprintf("[%s]:%s/%s", r.Get(addressField).String(), r.Get("to-ports").String(), r.Get("protocol").String())
			},
			ignoredIPFirewallFields,
		)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		respByChain[chain] = resp
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	return respByChain, nil
}

func (m *backend) ensureSNATRule(family core.IPFamily) (gjson.Result, error) {
	snatChainName := m.getSNATChainName()

	resp, err := m.client.Sync(
		fmt.Sprintf("/%s/firewall/nat", getAPIIPFamily(family)),
		[]api.Request{
			{
				"chain":       "srcnat",
				"action":      "jump",
				"jump-target": snatChainName,
				"comment":     "mikrolb: SNAT connections",
				"disabled":    false,
				"log":         false, // Fix: For patch to work
			},
		},
		api.Query{
			"chain":       "srcnat",
			"jump-target": snatChainName,
		},
		nil, // No custom filter
		nil, // No id function, as we only expect one rule
		ignoredIPFirewallFields,
	)
	if err != nil {
		return gjson.Result{}, err
	}

	return resp[0], nil
}

func (m *backend) ensureSNATSrcIPsList(family core.IPFamily, svc *core.Service) ([]gjson.Result, error) {
	var mask string
	switch family {
	case core.IPv4:
		// Fix: RouterOS removes a /32 from ipv4 addresses, so we must not add a mask here
	case core.IPv6:
		mask = "/128"
	default:
		return nil, fmt.Errorf("unsupported family: %s", family)
	}

	snatServiceIPsListName := m.getSNATServiceIPsListName(svc.UID)

	var reqs []api.Request
	for _, endpoint := range svc.Endpoints {
		for _, ip := range endpoint.IPs {
			if core.GetIPFamily(ip) != family {
				continue
			}

			reqs = append(reqs, api.Request{
				"list":     snatServiceIPsListName,
				"address":  ip.String() + mask,
				"comment":  fmt.Sprintf("mikrolb: service %s (%s)", svc.Name, svc.UID),
				"disabled": false,
			})
		}
	}

	resp, err := m.client.Sync(
		fmt.Sprintf("/%s/firewall/address-list", getAPIIPFamily(family)),
		reqs,
		api.Query{
			"list": snatServiceIPsListName,
		},
		nil, // No custom filter
		// Use address field as the unique identifier
		func(r gjson.Result) string {
			return r.Get("address").String()
		},
		ignoredIPListFields,
	)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (m *backend) ensureSNATServiceRule(family core.IPFamily, svc *core.Service) (gjson.Result, error) {
	var snatIP netip.Addr
	var addressField string
	switch family {
	case core.IPv4:
		snatIP = svc.SNATIPv4
		addressField = "to-addresses"
	case core.IPv6:
		snatIP = svc.SNATIPv6
		addressField = "to-address"
	default:
		return gjson.Result{}, fmt.Errorf("unsupported family: %s", family)
	}

	snatChainName := m.getSNATChainName()
	snatServiceIPsListName := m.getSNATServiceIPsListName(svc.UID)

	var reqs []api.Request
	if snatIP.IsValid() {
		reqs = append(reqs, api.Request{
			"chain":            snatChainName,
			"action":           "src-nat",
			"src-address-list": snatServiceIPsListName,
			addressField:       snatIP.String(),
			"comment":          fmt.Sprintf("mikrolb: SNAT for service %s (%s)", svc.Name, svc.UID),
			"disabled":         false,
			"log":              false, // Fix: For patch to work
		})
	}

	resp, err := m.client.Sync(
		fmt.Sprintf("/%s/firewall/nat", getAPIIPFamily(family)),
		reqs,
		api.Query{
			"chain":            snatChainName,
			"src-address-list": snatServiceIPsListName,
		},
		nil, // No custom filter
		// Use to-address field as the unique identifier
		func(r gjson.Result) string {
			return r.Get(addressField).String()
		},
		ignoredIPFirewallFields,
	)
	if err != nil {
		return gjson.Result{}, err
	}

	if len(resp) == 0 {
		return gjson.Result{}, nil
	}

	return resp[0], nil
}

func (m *backend) deleteIPAvertisement(ip netip.Addr) error {
	family := "ip"
	if ip.Is6() {
		family = "ipv6"
	}

	_, err := m.client.Sync(
		fmt.Sprintf("/%s/address", family),
		nil, // Delete all matching entries
		nil, // No specific query, as we can't filter here for a string prefix
		// Custom filter to only delete entries that are managed by mikrolb and match the IP address
		func(r gjson.Result) bool {
			return strings.HasPrefix(r.Get("comment").String(), "mikrolb:") && strings.HasPrefix(r.Get("address").String(), ip.String()+"/")
		},
		nil, // No id function required for pruning
		nil, // Ignored fields don't matter
	)

	return err
}

func (m *backend) cleanupLBIPsList(family core.IPFamily, serviceUID string) error {
	lbIPsListName := m.getLBIPsListName()

	_, err := m.client.Sync(
		fmt.Sprintf("/%s/firewall/address-list", getAPIIPFamily(family)),
		nil, // Delete all matching entries
		api.Query{
			"list": lbIPsListName,
		},
		// Custom filter to only delete entries with the service UID in the comment
		func(r gjson.Result) bool {
			commentStr := r.Get("comment").String()
			return strings.HasPrefix(commentStr, "mikrolb:") && strings.Contains(commentStr, serviceUID)
		},
		nil, // No id function required for deletion
		ignoredIPListFields,
	)

	return err
}

func (m *backend) deleteLBServiceRules(family core.IPFamily, serviceUID string) error {
	_, err := m.client.Sync(
		fmt.Sprintf("/%s/firewall/nat", getAPIIPFamily(family)),
		nil, // Delete all matching entries
		nil, // No specific query, as we can't filter here for a string prefix
		// Custom filter to only delete rules that match the service name in the comment
		func(r gjson.Result) bool {
			commentStr := r.Get("comment").String()
			return strings.HasPrefix(commentStr, "mikrolb:") && strings.Contains(commentStr, serviceUID)
		},
		nil, // No id function required for pruning
		nil, // Ignored fields don't matter
	)

	return err
}

func (m *backend) deleteSNATServiceRule(family core.IPFamily, serviceUID string) error {
	snatChainName := m.getSNATChainName()
	snatServiceIPsListName := m.getSNATServiceIPsListName(serviceUID)

	_, err := m.client.Sync(
		fmt.Sprintf("/%s/firewall/nat", getAPIIPFamily(family)),
		nil, // Delete all matching entries
		api.Query{
			"chain":            snatChainName,
			"src-address-list": snatServiceIPsListName,
		},
		nil, // No custom filter
		nil, // No id function required for pruning
		nil, // Ignored fields don't matter
	)

	return err
}

func (m *backend) deleteSNATSrcIPsList(family core.IPFamily, serviceUID string) error {
	snatServiceIPsListName := m.getSNATServiceIPsListName(serviceUID)

	_, err := m.client.Sync(
		fmt.Sprintf("/%s/firewall/address-list", getAPIIPFamily(family)),
		nil, // Delete all matching entries
		api.Query{
			"list": snatServiceIPsListName,
		},
		nil, // No custom filter
		nil, // No id function required for pruning
		nil, // Ignored fields don't matter
	)

	return err
}

func (m *backend) determineInterfaceForIP(ip netip.Addr) (string, error) {
	routesResp, err := m.client.Get("/routing/route", nil)
	if err != nil {
		return "", fmt.Errorf("failed to get routes: %w", err)
	}

	var matchedInterfaceName string
	var matchedRouteMetric uint64
	var matchedRouteDst netip.Prefix

	for _, route := range routesResp.Array() {
		dstStr := route.Get("dst-address").String()
		if dstStr == "" {
			continue
		}

		if !route.Get("active").Bool() {
			continue
		}

		dst, err := netip.ParsePrefix(dstStr)
		if err != nil {
			m.logger.V(2).Info("skipping route with invalid dst-address", "dst-address", dstStr)
			continue
		}

		// Check if the route matches the IP address (this will implicitly check for the correct IP family as well)
		if dst.Contains(ip) {
			interfaceName := route.Get("immediate-gw").String()
			if interfaceName == "" {
				continue
			}

			if matchedRouteDst.IsValid() {
				if dst.Bits() < matchedRouteDst.Bits() {
					continue // Skip less specific match
				}
				if dst.Bits() == matchedRouteDst.Bits() && route.Get("distance").Uint() >= matchedRouteMetric {
					continue // Skip same specificity but higher metric
				}
			}

			if strings.Contains(interfaceName, "%") {
				parts := strings.SplitN(interfaceName, "%", 2)
				matchedInterfaceName = parts[1]
			} else {
				matchedInterfaceName = interfaceName
			}

			matchedRouteMetric = route.Get("distance").Uint()
			matchedRouteDst = dst
		}
	}

	if matchedInterfaceName == "" {
		return "", errors.New("no matching route found")
	}

	return matchedInterfaceName, nil
}
