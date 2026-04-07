package routeros

import (
	"github.com/gerolf-vent/mikrolb/internal/core"
)

func getAPIIPFamily(family core.IPFamily) string {
	switch family {
	case core.IPv4:
		return "ip"
	case core.IPv6:
		return "ipv6"
	default:
		return ""
	}
}
