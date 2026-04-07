package core

import (
	"net/netip"
	"slices"

	"go.uber.org/zap/zapcore"
)

type EndpointPort struct {
	Port       uint16
	TargetPort uint16
	Protocol   string
}

type Endpoint struct {
	IPs   []netip.Addr
	Ports []EndpointPort
}

type Service struct {
	UID         string
	Name        string
	LBEnabled   bool
	LBIPs       []netip.Addr
	SNATEnabled bool
	SNATIPv4    netip.Addr
	SNATIPv6    netip.Addr
	Endpoints   []Endpoint
}

func (s *Service) GetAllIPs() []netip.Addr {
	ips := make([]netip.Addr, 0, len(s.LBIPs)+2)
	ips = append(ips, s.LBIPs...)
	if s.SNATIPv4.IsValid() {
		ips = append(ips, s.SNATIPv4)
	}
	if s.SNATIPv6.IsValid() {
		ips = append(ips, s.SNATIPv6)
	}
	return slices.Compact(ips)
}

func (s *Service) HasLBIP(family IPFamily) bool {
	for _, ip := range s.LBIPs {
		if GetIPFamily(ip) == family {
			return true
		}
	}
	return false
}

func (s *Service) HasSNATIP(family IPFamily) bool {
	if family == IPv4 {
		return s.SNATIPv4.IsValid()
	}
	return s.SNATIPv6.IsValid()
}

func (s Service) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("uid", s.UID)
	enc.AddString("name", s.Name)
	enc.AddBool("lbEnabled", s.LBEnabled)
	enc.AddArray("lbIPs", zapcore.ArrayMarshalerFunc(func(enc zapcore.ArrayEncoder) error {
		for _, ip := range s.LBIPs {
			enc.AppendString(ip.String())
		}
		return nil
	}))
	enc.AddBool("snatEnabled", s.SNATEnabled)
	if s.SNATIPv4.IsValid() {
		enc.AddString("snatIPv4", s.SNATIPv4.String())
	}
	if s.SNATIPv6.IsValid() {
		enc.AddString("snatIPv6", s.SNATIPv6.String())
	}
	return nil
}
