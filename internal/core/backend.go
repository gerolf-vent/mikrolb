package core

import (
	"net/netip"
)

type Backend interface {
	Check() (string, error)
	Setup() error
	EnsureIPAdvertisement(ip netip.Addr, interfaceName string) (string, error)
	DeleteIPAdvertisement(ip netip.Addr) error
	EnsureService(svc *Service) error
	DeleteService(name, uid string) error
}
