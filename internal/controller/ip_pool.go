package controller

import (
	"fmt"

	"github.com/gerolf-vent/mikrolb/internal/core"
)

func ParseIPPoolAddresses(addresses []string, ipFamily core.IPFamily, excludeCIDREdges bool) (core.IPRangeSet, []error) {
	var ipRanges, ipRangesExcluded []core.IPRange
	errors := make([]error, len(addresses))

	for i, s := range addresses {
		ipRange, isExcluded, err := core.ParseIPRange(s, excludeCIDREdges)
		if err != nil {
			errors[i] = err
			continue
		}
		if ipRange.IPFamily() != ipFamily {
			errors[i] = fmt.Errorf("IP family %s of address(es) does not match pool", ipRange.IPFamily())
			continue
		}
		if isExcluded {
			ipRangesExcluded = append(ipRangesExcluded, ipRange)
		} else {
			ipRanges = append(ipRanges, ipRange)
		}
	}

	ipRangesSet := core.NewIPRangeSet(ipRanges...)
	ipRangesSet.Subtract(ipRangesExcluded...)

	return ipRangesSet, errors
}
