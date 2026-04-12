package routeros

import (
	"fmt"
	"net"
	"net/netip"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

func GenerateGratuitousAdvertisement(mac []byte, ip netip.Addr) ([]byte, error) {
	if len(mac) != 6 {
		return nil, fmt.Errorf("invalid MAC address length: %d", len(mac))
	}
	if !ip.IsValid() {
		return nil, fmt.Errorf("invalid IP address")
	}

	ip = ip.Unmap() // Handle IPv4-mapped IPv6 addresses by converting them to IPv4

	buffer := gopacket.NewSerializeBuffer()
	opts := gopacket.SerializeOptions{
		FixLengths:       true,
		ComputeChecksums: true,
	}

	if ip.Is4() { // It's an IPv4 address
		ethLayer := &layers.Ethernet{
			SrcMAC:       mac,
			DstMAC:       net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
			EthernetType: layers.EthernetTypeARP,
		}

		arpLayer := &layers.ARP{
			AddrType:          layers.LinkTypeEthernet,
			Protocol:          layers.EthernetTypeIPv4,
			HwAddressSize:     6,
			ProtAddressSize:   4,
			Operation:         layers.ARPRequest,
			SourceHwAddress:   mac,
			SourceProtAddress: ip.AsSlice(),
			DstHwAddress:      net.HardwareAddr{0x00, 0x00, 0x00, 0x00, 0x00, 0x00},
			DstProtAddress:    ip.AsSlice(),
		}

		if err := gopacket.SerializeLayers(buffer, opts,
			ethLayer,
			arpLayer,
		); err != nil {
			return nil, err
		}
	} else { // It's an IPv6 address
		ethLayer := &layers.Ethernet{
			SrcMAC:       mac,
			DstMAC:       net.HardwareAddr{0x33, 0x33, 0x00, 0x00, 0x00, 0x01},
			EthernetType: layers.EthernetTypeIPv6,
		}

		ipv6Layer := &layers.IPv6{
			Version:    6,
			SrcIP:      ip.AsSlice(),
			DstIP:      net.IPv6linklocalallnodes,
			NextHeader: layers.IPProtocolICMPv6,
			HopLimit:   255,
		}

		icmpv6Layer := &layers.ICMPv6{
			TypeCode: layers.CreateICMPv6TypeCode(layers.ICMPv6TypeNeighborAdvertisement, 0),
		}
		icmpv6Layer.SetNetworkLayerForChecksum(ipv6Layer)

		icmpv6NALayer := &layers.ICMPv6NeighborAdvertisement{
			Flags:         0x20,
			TargetAddress: ip.AsSlice(),
			Options: []layers.ICMPv6Option{
				{
					Type: layers.ICMPv6OptTargetAddress,
					Data: mac,
				},
			},
		}

		if err := gopacket.SerializeLayers(buffer, opts,
			ethLayer,
			ipv6Layer,
			icmpv6Layer,
			icmpv6NALayer,
		); err != nil {
			return nil, err
		}
	}

	return buffer.Bytes(), nil
}
