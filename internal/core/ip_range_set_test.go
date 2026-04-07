package core

import (
	"net/netip"
	"reflect"
	"strings"
	"testing"
)

func TestIPRangeSet_NewIPRangeList_Empty(t *testing.T) {
	s := NewIPRangeSet()

	if s.HasRangesOfFamily(IPv4) || s.HasRangesOfFamily(IPv6) {
		t.Fatal("expected empty set")
	}

	if got := s.Ranges(); len(got) != 0 {
		t.Fatalf("expected no ranges, got %v", s.String())
	}
}

func TestIPRangeSet_NewIPRangeList_MergesByFamily(t *testing.T) {
	s := NewIPRangeSet(
		IPRange{start: netip.MustParseAddr("10.0.0.5"), end: netip.MustParseAddr("10.0.0.10")},
		IPRange{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.4")},
		IPRange{start: netip.MustParseAddr("fd00::3"), end: netip.MustParseAddr("fd00::6")},
		IPRange{start: netip.MustParseAddr("fd00::1"), end: netip.MustParseAddr("fd00::2")},
	)

	want := []string{"10.0.0.1-10.0.0.10", "fd00::1-fd00::6"}
	if got := s.String(); got != strings.Join(want, ", ") {
		t.Fatalf("String() = %v, want %v", got, strings.Join(want, ", "))
	}

	if !s.HasRangesOfFamily(IPv4) || !s.HasRangesOfFamily(IPv6) {
		t.Fatal("expected both IPv4 and IPv6 ranges")
	}
}

func TestIPRangeSet_HasRanges_InvalidFamily(t *testing.T) {
	s := NewIPRangeSet(IPRange{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.2")})

	if s.HasRangesOfFamily(IPFamily("")) {
		t.Fatal("expected HasRangesOfFamily(IPFamily(\"\")) to be false")
	}
	if s.HasRangesOfFamily(IPFamily("invalid")) {
		t.Fatal("expected HasRangesOfFamily(IPFamily(\"invalid\")) to be false")
	}
}

func TestIPRangeSet_Merge(t *testing.T) {
	s := NewIPRangeSet(
		IPRange{start: netip.MustParseAddr("10.0.0.10"), end: netip.MustParseAddr("10.0.0.12")},
		IPRange{start: netip.MustParseAddr("fd00::10"), end: netip.MustParseAddr("fd00::12")},
	)

	s.Merge(
		IPRange{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.9")},
		IPRange{start: netip.MustParseAddr("10.0.0.20"), end: netip.MustParseAddr("10.0.0.30")},
		IPRange{start: netip.MustParseAddr("10.0.0.31"), end: netip.MustParseAddr("10.0.0.35")},
		IPRange{start: netip.MustParseAddr("fd00::1"), end: netip.MustParseAddr("fd00::9")},
	)

	want := []string{
		"10.0.0.1-10.0.0.12",
		"10.0.0.20-10.0.0.35",
		"fd00::1-fd00::9",
		"fd00::10-fd00::12",
	}

	if got := s.String(); got != strings.Join(want, ", ") {
		t.Fatalf("String() = %v, want %v", got, strings.Join(want, ", "))
	}
}

func TestIPRangeSet_Merge_EmptyInputNoOp(t *testing.T) {
	s := NewIPRangeSet(
		IPRange{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.2")},
		IPRange{start: netip.MustParseAddr("fd00::1"), end: netip.MustParseAddr("fd00::2")},
	)

	before := s.String()
	s.Merge()
	after := s.String()

	if !reflect.DeepEqual(after, before) {
		t.Fatalf("Merge() changed state: before=%v, after=%v", before, after)
	}
}

func TestIPRangeSet_Subtract(t *testing.T) {
	tests := []struct {
		name     string
		base     []IPRange
		subtract []IPRange
		want     []string
	}{
		{
			name: "subtract from middle splits range",
			base: []IPRange{
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.10")},
			},
			subtract: []IPRange{
				{start: netip.MustParseAddr("10.0.0.4"), end: netip.MustParseAddr("10.0.0.6")},
			},
			want: []string{"10.0.0.1-10.0.0.3", "10.0.0.7-10.0.0.10"},
		},
		{
			name: "subtract whole range removes it",
			base: []IPRange{
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.10")},
			},
			subtract: []IPRange{
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.10")},
			},
			want: []string{},
		},
		{
			name: "subtract disjoint no change",
			base: []IPRange{
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.10")},
			},
			subtract: []IPRange{
				{start: netip.MustParseAddr("10.0.0.20"), end: netip.MustParseAddr("10.0.0.30")},
			},
			want: []string{"10.0.0.1-10.0.0.10"},
		},
		{
			name: "subtract affects only matching family",
			base: []IPRange{
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.5")},
				{start: netip.MustParseAddr("fd00::1"), end: netip.MustParseAddr("fd00::5")},
			},
			subtract: []IPRange{
				{start: netip.MustParseAddr("fd00::3"), end: netip.MustParseAddr("fd00::4")},
			},
			want: []string{"10.0.0.1-10.0.0.5", "fd00::1-fd00::2", "fd00::5"},
		},
		{
			name: "subtract with empty input no change",
			base: []IPRange{
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.5")},
			},
			subtract: nil,
			want:     []string{"10.0.0.1-10.0.0.5"},
		},
		{
			name: "subtract overlap at start trims left edge",
			base: []IPRange{
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.10")},
			},
			subtract: []IPRange{
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.3")},
			},
			want: []string{"10.0.0.4-10.0.0.10"},
		},
		{
			name: "subtract overlap at end trims right edge",
			base: []IPRange{
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.10")},
			},
			subtract: []IPRange{
				{start: netip.MustParseAddr("10.0.0.8"), end: netip.MustParseAddr("10.0.0.10")},
			},
			want: []string{"10.0.0.1-10.0.0.7"},
		},
		{
			name: "subtract superset removes whole base",
			base: []IPRange{
				{start: netip.MustParseAddr("10.0.0.3"), end: netip.MustParseAddr("10.0.0.7")},
			},
			subtract: []IPRange{
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.10")},
			},
			want: []string{},
		},
		{
			name: "subtract multiple holes from one range",
			base: []IPRange{
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.20")},
			},
			subtract: []IPRange{
				{start: netip.MustParseAddr("10.0.0.3"), end: netip.MustParseAddr("10.0.0.4")},
				{start: netip.MustParseAddr("10.0.0.6"), end: netip.MustParseAddr("10.0.0.8")},
				{start: netip.MustParseAddr("10.0.0.15"), end: netip.MustParseAddr("10.0.0.18")},
			},
			want: []string{"10.0.0.1-10.0.0.2", "10.0.0.5", "10.0.0.9-10.0.0.14", "10.0.0.19-10.0.0.20"},
		},
		{
			name: "subtract across multiple base ranges",
			base: []IPRange{
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.5")},
				{start: netip.MustParseAddr("10.0.0.10"), end: netip.MustParseAddr("10.0.0.15")},
			},
			subtract: []IPRange{
				{start: netip.MustParseAddr("10.0.0.3"), end: netip.MustParseAddr("10.0.0.12")},
			},
			want: []string{"10.0.0.1-10.0.0.2", "10.0.0.13-10.0.0.15"},
		},
		{
			name: "subtract merges overlapping subtractors before apply",
			base: []IPRange{
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.20")},
			},
			subtract: []IPRange{
				{start: netip.MustParseAddr("10.0.0.3"), end: netip.MustParseAddr("10.0.0.5")},
				{start: netip.MustParseAddr("10.0.0.6"), end: netip.MustParseAddr("10.0.0.10")},
			},
			want: []string{"10.0.0.1-10.0.0.2", "10.0.0.11-10.0.0.20"},
		},
		{
			name: "subtract handles unsorted and overlapping base and subtract input",
			base: []IPRange{
				{start: netip.MustParseAddr("10.0.0.10"), end: netip.MustParseAddr("10.0.0.12")},
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.5")},
				{start: netip.MustParseAddr("10.0.0.4"), end: netip.MustParseAddr("10.0.0.8")},
			},
			subtract: []IPRange{
				{start: netip.MustParseAddr("10.0.0.11"), end: netip.MustParseAddr("10.0.0.13")},
				{start: netip.MustParseAddr("10.0.0.3"), end: netip.MustParseAddr("10.0.0.4")},
				{start: netip.MustParseAddr("10.0.0.5"), end: netip.MustParseAddr("10.0.0.6")},
			},
			want: []string{"10.0.0.1-10.0.0.2", "10.0.0.7-10.0.0.8", "10.0.0.10"},
		},
		{
			name: "subtract adjacent lower range does not change base",
			base: []IPRange{
				{start: netip.MustParseAddr("10.0.0.10"), end: netip.MustParseAddr("10.0.0.20")},
			},
			subtract: []IPRange{
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.9")},
			},
			want: []string{"10.0.0.10-10.0.0.20"},
		},
		{
			name: "subtract adjacent upper range does not change base",
			base: []IPRange{
				{start: netip.MustParseAddr("10.0.0.10"), end: netip.MustParseAddr("10.0.0.20")},
			},
			subtract: []IPRange{
				{start: netip.MustParseAddr("10.0.0.21"), end: netip.MustParseAddr("10.0.0.30")},
			},
			want: []string{"10.0.0.10-10.0.0.20"},
		},
		{
			name: "subtract single ip at start keeps trailing range",
			base: []IPRange{
				{start: netip.MustParseAddr("10.0.0.10"), end: netip.MustParseAddr("10.0.0.12")},
			},
			subtract: []IPRange{
				{start: netip.MustParseAddr("10.0.0.10"), end: netip.MustParseAddr("10.0.0.10")},
			},
			want: []string{"10.0.0.11-10.0.0.12"},
		},
		{
			name: "subtract single ip at end keeps leading range",
			base: []IPRange{
				{start: netip.MustParseAddr("10.0.0.10"), end: netip.MustParseAddr("10.0.0.12")},
			},
			subtract: []IPRange{
				{start: netip.MustParseAddr("10.0.0.12"), end: netip.MustParseAddr("10.0.0.12")},
			},
			want: []string{"10.0.0.10-10.0.0.11"},
		},
		{
			name: "subtract one interval spanning several base ranges",
			base: []IPRange{
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.3")},
				{start: netip.MustParseAddr("10.0.0.5"), end: netip.MustParseAddr("10.0.0.7")},
				{start: netip.MustParseAddr("10.0.0.10"), end: netip.MustParseAddr("10.0.0.12")},
			},
			subtract: []IPRange{
				{start: netip.MustParseAddr("10.0.0.2"), end: netip.MustParseAddr("10.0.0.11")},
			},
			want: []string{"10.0.0.1", "10.0.0.12"},
		},
		{
			name: "subtract IPv6 interval spanning IPv6 base ranges",
			base: []IPRange{
				{start: netip.MustParseAddr("fd00::1"), end: netip.MustParseAddr("fd00::5")},
				{start: netip.MustParseAddr("fd00::10"), end: netip.MustParseAddr("fd00::12")},
			},
			subtract: []IPRange{
				{start: netip.MustParseAddr("fd00::3"), end: netip.MustParseAddr("fd00::10")},
			},
			want: []string{"fd00::1-fd00::2", "fd00::11-fd00::12"},
		},
		{
			name: "subtract many ranges from many ranges",
			base: []IPRange{
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.5")},
				{start: netip.MustParseAddr("10.0.0.8"), end: netip.MustParseAddr("10.0.0.12")},
				{start: netip.MustParseAddr("10.0.0.20"), end: netip.MustParseAddr("10.0.0.25")},
				{start: netip.MustParseAddr("10.0.0.30"), end: netip.MustParseAddr("10.0.0.35")},
				{start: netip.MustParseAddr("10.0.0.40"), end: netip.MustParseAddr("10.0.0.45")},
				{start: netip.MustParseAddr("10.0.0.50"), end: netip.MustParseAddr("10.0.0.60")},
			},
			subtract: []IPRange{
				{start: netip.MustParseAddr("10.0.0.2"), end: netip.MustParseAddr("10.0.0.3")},
				{start: netip.MustParseAddr("10.0.0.4"), end: netip.MustParseAddr("10.0.0.9")},
				{start: netip.MustParseAddr("10.0.0.11"), end: netip.MustParseAddr("10.0.0.11")},
				{start: netip.MustParseAddr("10.0.0.21"), end: netip.MustParseAddr("10.0.0.22")},
				{start: netip.MustParseAddr("10.0.0.24"), end: netip.MustParseAddr("10.0.0.33")},
				{start: netip.MustParseAddr("10.0.0.43"), end: netip.MustParseAddr("10.0.0.50")},
				{start: netip.MustParseAddr("10.0.0.52"), end: netip.MustParseAddr("10.0.0.53")},
				{start: netip.MustParseAddr("10.0.0.59"), end: netip.MustParseAddr("10.0.0.70")},
			},
			want: []string{
				"10.0.0.1",
				"10.0.0.10",
				"10.0.0.12",
				"10.0.0.20",
				"10.0.0.23",
				"10.0.0.34-10.0.0.35",
				"10.0.0.40-10.0.0.42",
				"10.0.0.51",
				"10.0.0.54-10.0.0.58",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := NewIPRangeSet(tt.base...)
			set.Subtract(tt.subtract...)

			got := set.String()
			if got != strings.Join(tt.want, ", ") {
				t.Fatalf("String() = %v, want %v", got, strings.Join(tt.want, ", "))
			}
		})
	}
}

func TestIPRangeSet_Intersections(t *testing.T) {
	tests := []struct {
		name string
		a    []IPRange
		b    []IPRange
		want []string
	}{
		{
			name: "both empty",
			want: []string{},
		},
		{
			name: "left empty",
			b: []IPRange{
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.10")},
			},
			want: []string{},
		},
		{
			name: "right empty",
			a: []IPRange{
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.10")},
			},
			want: []string{},
		},
		{
			name: "disjoint ranges no intersection",
			a: []IPRange{
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.5")},
			},
			b: []IPRange{
				{start: netip.MustParseAddr("10.0.0.8"), end: netip.MustParseAddr("10.0.0.10")},
			},
			want: []string{},
		},
		{
			name: "adjacent ranges do not intersect",
			a: []IPRange{
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.5")},
			},
			b: []IPRange{
				{start: netip.MustParseAddr("10.0.0.6"), end: netip.MustParseAddr("10.0.0.10")},
			},
			want: []string{},
		},
		{
			name: "single ip boundary intersection",
			a: []IPRange{
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.5")},
			},
			b: []IPRange{
				{start: netip.MustParseAddr("10.0.0.5"), end: netip.MustParseAddr("10.0.0.9")},
			},
			want: []string{"10.0.0.5"},
		},
		{
			name: "partial overlap",
			a: []IPRange{
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.10")},
			},
			b: []IPRange{
				{start: netip.MustParseAddr("10.0.0.4"), end: netip.MustParseAddr("10.0.0.15")},
			},
			want: []string{"10.0.0.4-10.0.0.10"},
		},
		{
			name: "one fully contained in another",
			a: []IPRange{
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.20")},
			},
			b: []IPRange{
				{start: netip.MustParseAddr("10.0.0.5"), end: netip.MustParseAddr("10.0.0.8")},
			},
			want: []string{"10.0.0.5-10.0.0.8"},
		},
		{
			name: "equal ranges",
			a: []IPRange{
				{start: netip.MustParseAddr("10.0.0.10"), end: netip.MustParseAddr("10.0.0.20")},
			},
			b: []IPRange{
				{start: netip.MustParseAddr("10.0.0.10"), end: netip.MustParseAddr("10.0.0.20")},
			},
			want: []string{"10.0.0.10-10.0.0.20"},
		},
		{
			name: "multiple ranges pairwise intersections",
			a: []IPRange{
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.5")},
				{start: netip.MustParseAddr("10.0.0.10"), end: netip.MustParseAddr("10.0.0.20")},
				{start: netip.MustParseAddr("10.0.0.30"), end: netip.MustParseAddr("10.0.0.40")},
			},
			b: []IPRange{
				{start: netip.MustParseAddr("10.0.0.3"), end: netip.MustParseAddr("10.0.0.12")},
				{start: netip.MustParseAddr("10.0.0.18"), end: netip.MustParseAddr("10.0.0.35")},
			},
			want: []string{"10.0.0.3-10.0.0.5", "10.0.0.10-10.0.0.12", "10.0.0.18-10.0.0.20", "10.0.0.30-10.0.0.35"},
		},
		{
			name: "mixed families only intersect within same family",
			a: []IPRange{
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.10")},
				{start: netip.MustParseAddr("fd00::1"), end: netip.MustParseAddr("fd00::10")},
			},
			b: []IPRange{
				{start: netip.MustParseAddr("10.0.0.5"), end: netip.MustParseAddr("10.0.0.12")},
				{start: netip.MustParseAddr("fd00::8"), end: netip.MustParseAddr("fd00::20")},
			},
			want: []string{"10.0.0.5-10.0.0.10", "fd00::8-fd00::10"},
		},
		{
			name: "unsorted and overlapping inputs are normalized before intersection",
			a: []IPRange{
				{start: netip.MustParseAddr("10.0.0.10"), end: netip.MustParseAddr("10.0.0.12")},
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.5")},
				{start: netip.MustParseAddr("10.0.0.5"), end: netip.MustParseAddr("10.0.0.8")},
			},
			b: []IPRange{
				{start: netip.MustParseAddr("10.0.0.7"), end: netip.MustParseAddr("10.0.0.9")},
				{start: netip.MustParseAddr("10.0.0.2"), end: netip.MustParseAddr("10.0.0.3")},
				{start: netip.MustParseAddr("10.0.0.11"), end: netip.MustParseAddr("10.0.0.11")},
			},
			want: []string{"10.0.0.2-10.0.0.3", "10.0.0.7-10.0.0.8", "10.0.0.11"},
		},
		{
			name: "many ranges intersect many ranges",
			a: []IPRange{
				{start: netip.MustParseAddr("10.0.0.1"), end: netip.MustParseAddr("10.0.0.4")},
				{start: netip.MustParseAddr("10.0.0.7"), end: netip.MustParseAddr("10.0.0.15")},
				{start: netip.MustParseAddr("10.0.0.20"), end: netip.MustParseAddr("10.0.0.24")},
				{start: netip.MustParseAddr("10.0.0.30"), end: netip.MustParseAddr("10.0.0.35")},
				{start: netip.MustParseAddr("10.0.0.40"), end: netip.MustParseAddr("10.0.0.50")},
			},
			b: []IPRange{
				{start: netip.MustParseAddr("10.0.0.3"), end: netip.MustParseAddr("10.0.0.8")},
				{start: netip.MustParseAddr("10.0.0.10"), end: netip.MustParseAddr("10.0.0.12")},
				{start: netip.MustParseAddr("10.0.0.14"), end: netip.MustParseAddr("10.0.0.22")},
				{start: netip.MustParseAddr("10.0.0.25"), end: netip.MustParseAddr("10.0.0.31")},
				{start: netip.MustParseAddr("10.0.0.33"), end: netip.MustParseAddr("10.0.0.34")},
				{start: netip.MustParseAddr("10.0.0.45"), end: netip.MustParseAddr("10.0.0.60")},
			},
			want: []string{"10.0.0.3-10.0.0.4", "10.0.0.7-10.0.0.8", "10.0.0.10-10.0.0.12", "10.0.0.14-10.0.0.15", "10.0.0.20-10.0.0.22", "10.0.0.30-10.0.0.31", "10.0.0.33-10.0.0.34", "10.0.0.45-10.0.0.50"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewIPRangeSet(tt.a...)
			b := NewIPRangeSet(tt.b...)

			intersection := a.Intersections(b)
			got := intersection.String()
			if got != strings.Join(tt.want, ", ") {
				t.Fatalf("Intersections() = %v, want %v", got, strings.Join(tt.want, ", "))
			}

			reverseIntersection := b.Intersections(a)
			gotReverse := reverseIntersection.String()
			if gotReverse != strings.Join(tt.want, ", ") {
				t.Fatalf("reverse Intersections() = %v, want %v", gotReverse, strings.Join(tt.want, ", "))
			}
		})
	}
}
