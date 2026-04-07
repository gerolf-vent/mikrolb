package core

import "net/netip"

type IPFamily string

const (
	IPv4 IPFamily = "IPv4"
	IPv6 IPFamily = "IPv6"
)

func GetIPFamily(ip netip.Addr) IPFamily {
	if ip.Is4() {
		return IPv4
	} else if ip.Is6() {
		return IPv6
	}
	return ""
}
