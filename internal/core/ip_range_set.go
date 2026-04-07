package core

import (
	"math/big"
	"net/netip"
	"slices"
	"strings"
)

type IPRangeSet struct {
	ranges4 []IPRange
	ranges6 []IPRange
}

func NewIPRangeSet(rs ...IPRange) IPRangeSet {
	s := IPRangeSet{}
	s.Merge(rs...)
	return s
}

func (s *IPRangeSet) HasRangesOfFamily(ipFamily IPFamily) bool {
	switch ipFamily {
	case IPv4:
		return len(s.ranges4) > 0
	case IPv6:
		return len(s.ranges6) > 0
	default:
		return false
	}
}

func (s *IPRangeSet) Ranges() []IPRange {
	return append(s.ranges4, s.ranges6...)
}

func (s *IPRangeSet) RangesForFamily(ipFamily IPFamily) []IPRange {
	switch ipFamily {
	case IPv4:
		return s.ranges4
	case IPv6:
		return s.ranges6
	default:
		return nil
	}
}

func (s *IPRangeSet) Merge(rs ...IPRange) {
	if len(rs) == 0 {
		return
	}

	rs4 := make([]IPRange, 0, len(rs))
	rs6 := make([]IPRange, 0, len(rs))

	for _, r := range rs {
		switch r.IPFamily() {
		case IPv4:
			rs4 = append(rs4, r)
		case IPv6:
			rs6 = append(rs6, r)
		}
	}

	s.ranges4 = s.merge(append(s.ranges4, rs4...))
	s.ranges6 = s.merge(append(s.ranges6, rs6...))
}

func (s *IPRangeSet) Subtract(rs ...IPRange) {
	if len(rs) == 0 {
		return
	}

	rs4 := make([]IPRange, 0, len(rs))
	rs6 := make([]IPRange, 0, len(rs))

	for _, r := range rs {
		switch r.IPFamily() {
		case IPv4:
			rs4 = append(rs4, r)
		case IPv6:
			rs6 = append(rs6, r)
		}
	}

	s.ranges4 = s.subtract(s.ranges4, s.merge(rs4))
	s.ranges6 = s.subtract(s.ranges6, s.merge(rs6))
}

func (s *IPRangeSet) Intersections(rs IPRangeSet) IPRangeSet {
	return IPRangeSet{
		ranges4: s.intersection(s.ranges4, rs.ranges4),
		ranges6: s.intersection(s.ranges6, rs.ranges6),
	}
}

func (s *IPRangeSet) Count(ipFamily IPFamily) *big.Int {
	total := big.NewInt(0)
	switch ipFamily {
	case IPv4:
		for _, r := range s.ranges4 {
			total.Add(total, r.Count())
		}
	case IPv6:
		for _, r := range s.ranges6 {
			total.Add(total, r.Count())
		}
	}
	return total
}

func (s *IPRangeSet) Contains(ip netip.Addr) bool {
	family := GetIPFamily(ip)
	for _, r := range s.RangesForFamily(family) {
		if r.Contains(ip) {
			return true
		}
	}
	return false
}

func (s *IPRangeSet) String() string {
	var parts []string
	for _, r := range s.ranges4 {
		parts = append(parts, r.String())
	}
	for _, r := range s.ranges6 {
		parts = append(parts, r.String())
	}
	return strings.Join(parts, ", ")
}

func (s *IPRangeSet) merge(rs []IPRange) []IPRange {
	if len(rs) == 0 {
		return nil
	}

	slices.SortFunc(rs, func(a, b IPRange) int {
		return a.start.Compare(b.start)
	})

	var merged []IPRange
	current := rs[0]

	for i := 1; i < len(rs); i++ {
		next := rs[i]

		// Check if they overlap or are exactly adjacent.
		// They intersect if next.start <= current.end + 1
		if next.start.Compare(current.end) <= 0 || next.start == current.end.Next() {
			// Extend current.end if next.end is greater
			if current.end.Compare(next.end) < 0 {
				current.end = next.end
			}
		} else {
			// No overlap
			merged = append(merged, current)
			current = next
		}
	}

	return append(merged, current)
}

func (s *IPRangeSet) subtract(rs1, rs2 []IPRange) []IPRange {
	if len(rs1) == 0 {
		return nil
	}
	if len(rs2) == 0 {
		return rs1
	}

	final := make([]IPRange, 0, len(rs1))

	i := 0
	j := 0
	current := rs1[0]
	for i < len(rs1) && j < len(rs2) {
		sub := rs2[j]

		if current.end.Compare(sub.start) < 0 {
			// current is completely before sub, so keep it
			final = append(final, current)
		} else if current.start.Compare(sub.start) < 0 {
			// current overlaps with sub, keep the part before sub
			final = append(final, IPRange{
				start: current.start,
				end:   sub.start.Prev(),
			})
		}

		// advance the sub range if it ends before or at the end of the current range
		if sub.end.Compare(current.end) <= 0 {
			j++
		}

		if current.end.Compare(sub.end) <= 0 {
			// advance the current range if it ends before or at the end of the sub range
			i++
			if i < len(rs1) {
				current = rs1[i]
			}
		} else {
			// current extends beyond sub, so keep the part after sub and advance the sub range
			current.start = sub.end.Next()
		}
	}

	// Append any remaining ranges from rs1
	if i < len(rs1) {
		final = append(final, current)
		final = append(final, rs1[i+1:]...)
	}

	return final
}

func (s *IPRangeSet) intersection(rs1, rs2 []IPRange) []IPRange {
	if len(rs1) == 0 || len(rs2) == 0 {
		return nil
	}

	var intersected []IPRange

	i := 0
	j := 0
	for i < len(rs1) && j < len(rs2) {
		r1 := rs1[i]
		r2 := rs2[j]

		if r1.end.Compare(r2.start) < 0 {
			// r1 is completely before r2, advance r1
			i++
		} else if r2.end.Compare(r1.start) < 0 {
			// r2 is completely before r1, advance r2
			j++
		} else {
			// They overlap, add the intersection
			start := r1.start
			if r2.start.Compare(start) > 0 {
				start = r2.start
			}
			end := r1.end
			if r2.end.Compare(end) < 0 {
				end = r2.end
			}
			if start.Compare(end) <= 0 {
				intersected = append(intersected, IPRange{start: start, end: end})
			}

			// Advance the range that ends first
			if r1.end.Compare(r2.end) <= 0 {
				i++
			} else {
				j++
			}
		}
	}

	return intersected
}
