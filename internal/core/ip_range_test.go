package core

import (
	"math/big"
	"net/netip"
	"testing"
)

func TestParseIPRange_Range(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantStart string
		wantEnd   string
	}{
		{"simple v4 range", "10.0.0.1-10.0.0.5", "10.0.0.1", "10.0.0.5"},
		{"single IP range", "10.0.0.1-10.0.0.1", "10.0.0.1", "10.0.0.1"},
		{"v6 range", "fd00::1-fd00::ff", "fd00::1", "fd00::ff"},
		{"whitespace trimmed", " 10.0.0.1 - 10.0.0.5 ", "10.0.0.1", "10.0.0.5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _, err := ParseIPRange(tt.input, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if r.start != netip.MustParseAddr(tt.wantStart) {
				t.Errorf("start = %s, want %s", r.start, tt.wantStart)
			}
			if r.end != netip.MustParseAddr(tt.wantEnd) {
				t.Errorf("end = %s, want %s", r.end, tt.wantEnd)
			}
		})
	}
}

func TestParseIPRange_RangeErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"invalid start", "invalid-10.0.0.5"},
		{"invalid end", "10.0.0.1-invalid"},
		{"mixed families", "10.0.0.1-fd00::1"},
		{"reversed range", "10.0.0.5-10.0.0.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := ParseIPRange(tt.input, false)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestParseIPRange_CIDR(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		excludeEdges bool
		wantStart    string
		wantEnd      string
	}{
		{"v4 /24", "192.168.1.0/24", false, "192.168.1.0", "192.168.1.255"},
		{"v4 /24 exclude edges", "192.168.1.0/24", true, "192.168.1.1", "192.168.1.254"},
		{"v4 /30", "10.0.0.0/30", false, "10.0.0.0", "10.0.0.3"},
		{"v4 /30 exclude edges", "10.0.0.0/30", true, "10.0.0.1", "10.0.0.2"},
		{"v4 /16", "10.1.0.0/16", false, "10.1.0.0", "10.1.255.255"},
		{"v6 /120", "fd00::/120", false, "fd00::", "fd00::ff"},
		{"v6 /120 exclude edges", "fd00::/120", true, "fd00::1", "fd00::fe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _, err := ParseIPRange(tt.input, tt.excludeEdges)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if r.start != netip.MustParseAddr(tt.wantStart) {
				t.Errorf("start = %s, want %s", r.start, tt.wantStart)
			}
			if r.end != netip.MustParseAddr(tt.wantEnd) {
				t.Errorf("end = %s, want %s", r.end, tt.wantEnd)
			}
		})
	}
}

func TestParseIPRange_CIDRErrors(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		excludeEdges bool
	}{
		{"invalid CIDR", "10.0.0.0/abc", false},
		{"v4 /31 exclude edges no usable", "10.0.0.0/31", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := ParseIPRange(tt.input, tt.excludeEdges)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestParseIPRange_SingleIP(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"v4", "10.0.0.1", "10.0.0.1"},
		{"v6", "fd00::1", "fd00::1"},
		{"whitespace", "  10.0.0.1  ", "10.0.0.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _, err := ParseIPRange(tt.input, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			want := netip.MustParseAddr(tt.want)
			if r.start != want || r.end != want {
				t.Errorf("got %s-%s, want %s-%s", r.start, r.end, want, want)
			}
		})
	}
}

func TestParseIPRange_SingleIPError(t *testing.T) {
	_, _, err := ParseIPRange("not-an-ip", false)
	if err == nil {
		t.Fatal("expected error for invalid IP")
	}
}

func TestParseIPRange_IsExcluded(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantIsExcluded bool
	}{
		{name: "plain range", input: "10.0.0.1-10.0.0.5", wantIsExcluded: false},
		{name: "excluded range", input: "!10.0.0.1-10.0.0.5", wantIsExcluded: true},
		{name: "excluded single ip with whitespace", input: "  ! 10.0.0.1 ", wantIsExcluded: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, isExcluded, err := ParseIPRange(tt.input, false)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if isExcluded != tt.wantIsExcluded {
				t.Fatalf("isExcluded = %v, want %v", isExcluded, tt.wantIsExcluded)
			}
		})
	}
}

func TestIPRange_IPFamily(t *testing.T) {
	v4, _, _ := ParseIPRange("10.0.0.1-10.0.0.5", false)
	if v4.IPFamily() != "IPv4" {
		t.Errorf("expected IPv4, got %s", v4.IPFamily())
	}

	v6, _, _ := ParseIPRange("fd00::1-fd00::5", false)
	if v6.IPFamily() != "IPv6" {
		t.Errorf("expected IPv6, got %s", v6.IPFamily())
	}
}

func TestIPRange_Contains(t *testing.T) {
	r, _, _ := ParseIPRange("10.0.0.5-10.0.0.10", false)

	tests := []struct {
		addr string
		want bool
	}{
		{"10.0.0.4", false},
		{"10.0.0.5", true},
		{"10.0.0.7", true},
		{"10.0.0.10", true},
		{"10.0.0.11", false},
		{"192.168.1.1", false},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			got := r.Contains(netip.MustParseAddr(tt.addr))
			if got != tt.want {
				t.Errorf("Contains(%s) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
}

func TestIPRange_Contains_SingleIP(t *testing.T) {
	r, _, _ := ParseIPRange("10.0.0.5", false)

	if !r.Contains(netip.MustParseAddr("10.0.0.5")) {
		t.Error("single-IP range should contain itself")
	}
	if r.Contains(netip.MustParseAddr("10.0.0.4")) {
		t.Error("single-IP range should not contain adjacent IP")
	}
}

func TestIPRange_Overlaps(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{name: "v4 overlap", a: "10.0.0.1-10.0.0.10", b: "10.0.0.5-10.0.0.20", want: true},
		{name: "v4 adjacent no overlap", a: "10.0.0.1-10.0.0.10", b: "10.0.0.11-10.0.0.20", want: false},
		{name: "v4 disjoint", a: "10.0.0.1-10.0.0.10", b: "10.0.1.1-10.0.1.20", want: false},
		{name: "single inside range", a: "10.0.0.5", b: "10.0.0.1-10.0.0.10", want: true},
		{name: "ipv6 overlap", a: "fd00::1-fd00::10", b: "fd00::8-fd00::20", want: true},
		{name: "mixed families", a: "10.0.0.1-10.0.0.10", b: "fd00::1-fd00::10", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ra, _, err := ParseIPRange(tt.a, false)
			if err != nil {
				t.Fatalf("ParseIPRange(a) failed: %v", err)
			}

			rb, _, err := ParseIPRange(tt.b, false)
			if err != nil {
				t.Fatalf("ParseIPRange(b) failed: %v", err)
			}

			if got := ra.Overlaps(rb); got != tt.want {
				t.Fatalf("Overlaps() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIPRange_Iter(t *testing.T) {
	r, _, _ := ParseIPRange("10.0.0.1-10.0.0.5", false)

	var got []netip.Addr
	for ip := range r.Iter([16]byte{}) {
		got = append(got, ip)
	}

	want := []netip.Addr{
		netip.MustParseAddr("10.0.0.1"),
		netip.MustParseAddr("10.0.0.2"),
		netip.MustParseAddr("10.0.0.3"),
		netip.MustParseAddr("10.0.0.4"),
		netip.MustParseAddr("10.0.0.5"),
	}

	if len(got) != len(want) {
		t.Fatalf("got %d IPs, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestIPRange_Iter_SingleIP(t *testing.T) {
	r, _, _ := ParseIPRange("10.0.0.1", false)

	var got []netip.Addr
	for ip := range r.Iter([16]byte{}) {
		got = append(got, ip)
	}

	if len(got) != 1 {
		t.Fatalf("expected 1 IP, got %d", len(got))
	}
	if got[0] != netip.MustParseAddr("10.0.0.1") {
		t.Errorf("got %s, want 10.0.0.1", got[0])
	}
}

func TestIPRange_Iter_Deterministic(t *testing.T) {
	r, _, _ := ParseIPRange("10.0.0.1-10.0.0.5", false)

	tests := []struct {
		name      string
		startHash uint64
		want      []string
	}{
		{
			name:      "hash 0 - starts at beginning",
			startHash: 0,
			want:      []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4", "10.0.0.5"},
		},
		{
			name:      "hash 2 - starts at index 2 (10.0.0.3)",
			startHash: 2,
			want:      []string{"10.0.0.3", "10.0.0.4", "10.0.0.5", "10.0.0.1", "10.0.0.2"},
		},
		{
			name:      "hash 4 - starts at last element",
			startHash: 4,
			want:      []string{"10.0.0.5", "10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4"},
		},
		{
			name:      "hash 5 - modulo 5 wraps perfectly to 0",
			startHash: 5,
			want:      []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4", "10.0.0.5"},
		},
		{
			name:      "hash 12 - effectively index 2 (12 % 5)",
			startHash: 12,
			want:      []string{"10.0.0.3", "10.0.0.4", "10.0.0.5", "10.0.0.1", "10.0.0.2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for ip := range r.Iter(hashUint(tt.startHash)) {
				got = append(got, ip.String())
			}

			if len(got) != len(tt.want) {
				t.Fatalf("got %d IPs, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got[%d] = %s, want %s", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestIPRange_Iter_Deterministic_CrossOctet(t *testing.T) {
	r, _, _ := ParseIPRange("10.0.0.254-10.0.1.1", false) // 4 IPs: 10.0.0.254, .255, .1.0, .1.1

	var got []string
	for ip := range r.Iter(hashUint(2)) { // Should start at 10.0.1.0
		t.Logf("IP: %s", ip.String())
		got = append(got, ip.String())
	}

	want := []string{"10.0.1.0", "10.0.1.1", "10.0.0.254", "10.0.0.255"}

	if len(got) != len(want) {
		t.Fatalf("got %d IPs, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestIPRange_Iter_Deterministic_IPv6(t *testing.T) {
	r, _, _ := ParseIPRange("fd00::1-fd00::5", false)

	tests := []struct {
		name      string
		startHash uint64
		want      []string
	}{
		{
			name:      "hash 0 - starts at beginning",
			startHash: 0,
			want:      []string{"fd00::1", "fd00::2", "fd00::3", "fd00::4", "fd00::5"},
		},
		{
			name:      "hash 2 - starts at index 2 (fd00::3)",
			startHash: 2,
			want:      []string{"fd00::3", "fd00::4", "fd00::5", "fd00::1", "fd00::2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for ip := range r.Iter(hashUint(tt.startHash)) {
				got = append(got, ip.String())
			}

			if len(got) != len(tt.want) {
				t.Fatalf("got %d IPs, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got[%d] = %s, want %s", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestIPRange_Iter_Deterministic_IPv6_CrossOctet(t *testing.T) {
	r, _, _ := ParseIPRange("fd00::fffe-fd00::1:1", false) // 4 IPs: fd00::fffe, fd00::ffff, fd00::1:0, fd00::1:1

	var got []string
	for ip := range r.Iter(hashUint(2)) { // Should start at fd00::1:0
		got = append(got, ip.String())
	}

	want := []string{"fd00::1:0", "fd00::1:1", "fd00::fffe", "fd00::ffff"}

	if len(got) != len(want) {
		t.Fatalf("got %d IPs, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %s, want %s", i, got[i], want[i])
		}
	}
}

func TestIPRange_Count(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  uint64
	}{
		{"single IP", "10.0.0.1", 1},
		{"two IPs", "10.0.0.1-10.0.0.2", 2},
		{"five IPs", "10.0.0.1-10.0.0.5", 5},
		{"full /24", "192.168.1.0/24", 256},
		{"full /24 exclude edges", "192.168.1.0-192.168.1.255", 256},
		{"/30", "10.0.0.0-10.0.0.3", 4},
		{"cross octet boundary", "10.0.0.200-10.0.1.10", 67},
		{"/16", "10.1.0.0/16", 65536},
		{"v6 single", "fd00::1", 1},
		{"v6 small range", "fd00::1-fd00::5", 5},
		{"v6 /120", "fd00::/120", 256},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _, err := ParseIPRange(tt.input, false)
			if err != nil {
				t.Fatalf("ParseIPRange error: %v", err)
			}
			got := r.Count()
			if got.Cmp(big.NewInt(int64(tt.want))) != 0 {
				t.Errorf("Count() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestIPRange_Iter_EarlyBreak(t *testing.T) {
	r, _, _ := ParseIPRange("10.0.0.1-10.0.0.100", false)

	count := 0
	for range r.Iter([16]byte{}) {
		count++
		if count == 3 {
			break
		}
	}
	if count != 3 {
		t.Errorf("expected to break after 3 iterations, got %d", count)
	}
}

func hashUint(n uint64) [16]byte {
	var h [16]byte
	h[15] = byte(n)
	return h
}

func TestIPRange_Iter_LargeIPv6(t *testing.T) {
	// A /64 IPv6 range contains 2^64 addresses.
	// We can test whether the 128-bit startHash handles modulo 2^64 correctly
	// by setting the upper 64 bits of the hash to some arbitrary large number,
	// and verifying the offset isolates to the lower 64 bits.
	r, _, err := ParseIPRange("fd00::/64", false)
	if err != nil {
		t.Fatalf("ParseIPRange failed: %v", err)
	}

	t.Run("offset isolated to 5", func(t *testing.T) {
		var startHash [16]byte
		// Upper 64 bits to max
		for i := 0; i < 8; i++ {
			startHash[i] = 0xff
		}
		// Lower 64 bits to exactly 5
		startHash[15] = 5

		var got []string
		count := 0
		for ip := range r.Iter(startHash) {
			got = append(got, ip.String())
			count++
			if count == 3 {
				break
			}
		}

		want := []string{
			"fd00::5",
			"fd00::6",
			"fd00::7",
		}
		if len(got) != len(want) {
			t.Fatalf("got %d items, want %d", len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("At index %d, got %s, want %s", i, got[i], want[i])
			}
		}
	})

	t.Run("wrap around large subnet", func(t *testing.T) {
		var startHash [16]byte
		// Upper 64 bits arbitrary
		for i := 0; i < 8; i++ {
			startHash[i] = 0xaa
		}
		// Lower 64 bits to all 1s (2^64 - 1), which is the last IP in the /64 subnet
		for i := 8; i < 16; i++ {
			startHash[i] = 0xff
		}

		var got []string
		count := 0
		for ip := range r.Iter(startHash) {
			got = append(got, ip.String())
			count++
			if count == 3 {
				break
			}
		}

		want := []string{
			"fd00::ffff:ffff:ffff:ffff",
			"fd00::",
			"fd00::1",
		}
		if len(got) != len(want) {
			t.Fatalf("got %d items, want %d", len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("At index %d, got %s, want %s", i, got[i], want[i])
			}
		}
	})
}
