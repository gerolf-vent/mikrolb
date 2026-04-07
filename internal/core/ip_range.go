package core

import (
	"errors"
	"fmt"
	"math/big"
	"net/netip"
	"strings"
)

type IPRange struct {
	start netip.Addr
	end   netip.Addr
}

func NewIPRange(start, end netip.Addr) (IPRange, error) {
	if start.BitLen() != end.BitLen() {
		return IPRange{}, fmt.Errorf("mixed address families: %s and %s", start, end)
	}

	if end.Less(start) {
		return IPRange{}, fmt.Errorf("range end %s is before start %s", end, start)
	}

	return IPRange{
		start: start,
		end:   end,
	}, nil
}

func ParseIPRange(s string, excludeCIDREdges bool) (ipRange IPRange, isExcluded bool, err error) {
	s = strings.TrimSpace(s)

	if strings.HasPrefix(s, "!") {
		s = strings.TrimPrefix(s, "!")
		s = strings.TrimSpace(s)
		isExcluded = true
	}

	// Check for format "10.1.0.1-10.1.0.100"
	if strings.Contains(s, "-") {
		parts := strings.SplitN(s, "-", 2)

		start, err2 := netip.ParseAddr(strings.TrimSpace(parts[0]))
		if err2 != nil {
			err = err2
			return
		}

		end, err2 := netip.ParseAddr(strings.TrimSpace(parts[1]))
		if err2 != nil {
			err = err2
			return
		}

		ipRange, err = NewIPRange(start, end)
		return
	}

	// Check for format "10.1.0.0/16"
	if strings.Contains(s, "/") {
		prefix, err2 := netip.ParsePrefix(s)
		if err2 != nil {
			err = err2
			return
		}

		start := prefix.Masked().Addr()
		endS := start.AsSlice()

		count := start.BitLen() - prefix.Bits()
		j := start.BitLen()/8 - 1

		for count > 0 && j >= 0 {
			if count >= 8 {
				endS[j] = 0xff
				j--
				count -= 8
			} else {
				offset := byte(0xff >> (8 - count))
				endS[j] |= offset
				count = 0
			}
		}

		end, ok := netip.AddrFromSlice(endS)
		if !ok {
			err = errors.New("invalid CIDR end address")
			return
		}

		if excludeCIDREdges {
			start = start.Next()
			end = end.Prev()
			if !start.IsValid() || !end.IsValid() || end.Less(start) {
				err = errors.New("CIDR has no usable address(es) after excluding first and last")
				return
			}
		}

		ipRange, err = NewIPRange(start, end)
		return
	}

	// Assume it's a single IP address
	ip, err2 := netip.ParseAddr(s)
	if err2 != nil {
		err = err2
		return
	}
	ipRange, err = NewIPRange(ip, ip)
	return
}

func (r *IPRange) IPFamily() IPFamily {
	return GetIPFamily(r.start)
}

func (r *IPRange) Contains(addr netip.Addr) bool {
	return r.start.Prev().Less(addr) && addr.Less(r.end.Next())
}

func (r *IPRange) Overlaps(other IPRange) bool {
	if r.IPFamily() != other.IPFamily() {
		return false
	}

	return r.start.Compare(other.end) <= 0 && other.start.Compare(r.end) <= 0
}

func (r IPRange) String() string {
	if r.start == r.end {
		return r.start.String()
	}

	return fmt.Sprintf("%s-%s", r.start, r.end)
}

func (r *IPRange) Count() *big.Int {
	startInt := big.NewInt(0).SetBytes(r.start.AsSlice())
	endInt := big.NewInt(0).SetBytes(r.end.AsSlice())

	diff := big.NewInt(0).Sub(endInt, startInt)
	return diff.Add(diff, big.NewInt(1))
}

func (r *IPRange) Iter(startHash [16]byte) func(yield func(netip.Addr) bool) {
	start := r.start
	startHashInt := big.NewInt(0).SetBytes(startHash[:])
	if startHashInt.Sign() != 0 {
		offset := big.NewInt(0).Mod(startHashInt, r.Count())

		// Convert start IP to big.Int and add the offset
		startInt := big.NewInt(0).SetBytes(start.AsSlice())
		startInt.Add(startInt, offset)

		// Convert the big.Int back to a byte slice of the correct length (4 or 16 bytes)
		ipLen := start.BitLen() / 8
		buf := make([]byte, ipLen)
		startInt.FillBytes(buf)

		var ok bool
		start, ok = netip.AddrFromSlice(buf)
		if !ok {
			panic("invalid start address after applying hash offset")
		}
	}

	end := r.end

	return func(yield func(netip.Addr) bool) {
		addr := start
		for {
			if !yield(addr) {
				return
			}
			if addr == end {
				if start == r.start || end != r.end {
					break
				} else {
					// Wrapped around, now iterate from the beginning to the start point
					addr = r.start
					end = start.Prev()
					continue
				}
			}
			addr = addr.Next()
		}
	}
}
