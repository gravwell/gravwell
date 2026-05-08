/*************************************************************************
 * Copyright 2019 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package ipgen implements some high speed pre-populated IP address generator functions
package ipgen

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"net"

	rd "github.com/Pallinder/go-randomdata"
)

// A V4Gen is a generator for IPv4 addresses
type V4Gen struct {
	subnets []*net.IPNet
	ips     []uint32
	masks   []uint32
}

var (
	v4g *V4Gen
	v6g *V6Gen
)

func init() {
	v4g, _ = RandomWeightedV4Generator(3)
	v6g, _ = RandomWeightedV6Generator(3)
}

func IPv4() net.IP {
	return v4g.IP()
}

func IPv6() net.IP {
	return v6g.IP()
}

// NewV4Generator will return an IPv4 address generator which uses
// the specified subnets to generate traffic. A subnet may be included
// in the argument slice multiple times in order to generate proportionally
// more addresses from that network. Specifying a subnet 0.0.0.0/0 will
// generate addresses from the entire IPv4 space
func NewV4Generator(subnets []*net.IPNet) (*V4Gen, error) {
	if len(subnets) == 0 {
		return nil, errors.New("Must specify at least one subnet")
	}
	var ips, masks []uint32
	for _, sn := range subnets {
		if sn.IP.To4() == nil {
			return nil, fmt.Errorf("Subnet does not appear to be a v4 subnet: %v", sn)
		}
		ips = append(ips, binary.BigEndian.Uint32(sn.IP))
		var m []byte
		for _, b := range sn.Mask {
			m = append(m, ^b)
		}
		masks = append(masks, binary.BigEndian.Uint32(m))
	}
	if len(subnets) != len(ips) || len(subnets) != len(masks) {
		return nil, errors.New("Something went wrong in instantiating the generator")
	}
	return &V4Gen{subnets: subnets, ips: ips, masks: masks}, nil
}

// RandomWeightedV4Generator builds a generator with weighted subnets intended
// to approximate real-world traffic. Given a count argument, the function
// will instantiate random subnets of the following weights:
// * 30% of traffic from a subnet of size /16 to /26
// * 10% of traffic from between 2 and 5 subnets of size /16 to /24
// * All remaining traffic from random subnets between /8 and /16
// Specifying a higher count will result in a greater number in the final category.
func RandomWeightedV4Generator(count int) (*V4Gen, error) {
	var subnets []*net.IPNet

	// First generate a small subnet that will make most of our IPs
	netstr := rd.IpV4Address() + fmt.Sprintf("/%d", 16+rand.Intn(10))
	_, n, err := net.ParseCIDR(netstr)
	if err != nil {
		return nil, err
	}
	// Now put that subnet in several times
	for i := 0; i < int(3*count/10); i++ {
		subnets = append(subnets, n)
	}

	// Now generate a few more that will be pretty frequent
	for i := 0; i < 2+rand.Intn(3); i++ {
		netstr = rd.IpV4Address() + fmt.Sprintf("/%d", 16+rand.Intn(8))
		_, n, err = net.ParseCIDR(netstr)
		if err != nil {
			return nil, err
		}
		// Now put that subnet in several times
		for j := 0; j < int(count/10); j++ {
			subnets = append(subnets, n)
		}
	}

	// Now add some more one-offs
	for len(subnets) < count {
		netstr = rd.IpV4Address() + fmt.Sprintf("/%d", 8+rand.Intn(8))
		_, n, err = net.ParseCIDR(netstr)
		if err != nil {
			return nil, err
		}
		subnets = append(subnets, n)
	}
	return NewV4Generator(subnets)
}

// IP generates an IPv4 address based on the parameters of the generator
func (g *V4Gen) IP() net.IP {
	// First get a completely random IP
	ip := make(net.IP, 4)
	idx := rand.Intn(len(g.subnets))
	binary.BigEndian.PutUint32(ip, (rand.Uint32()&g.masks[idx])|g.ips[idx])
	return ip
}

// A V6Gen is a generator for IPv6 addresses
type V6Gen struct {
	subnets []*net.IPNet
	ips     [][]byte
	masks   [][]byte
}

// NewV6Generator will return an IPv6 address generator which uses
// the specified subnets to generate traffic. A subnet may be included
// in the argument slice multiple times in order to generate proportionally
// more addresses from that network. Specifying a subnet ::0/0 will
// generate addresses from the entire IPv6 space
func NewV6Generator(subnets []*net.IPNet) (*V6Gen, error) {
	gen := &V6Gen{}
	if len(subnets) == 0 {
		return nil, errors.New("Must specify at least one subnet")
	}

	var ips, masks [][]byte
	for _, sn := range subnets {
		if sn.IP.To16() == nil {
			return nil, fmt.Errorf("Subnet does not appear to be a v6 subnet: %v", sn)
		}

		ips = append(ips, []byte(sn.IP))

		var m []byte
		for _, b := range sn.Mask {
			m = append(m, ^b)
		}
		masks = append(masks, m)
	}
	if len(subnets) != len(ips) || len(subnets) != len(masks) {
		return nil, errors.New("Something went wrong in instantiating the generator")
	}

	gen.subnets = subnets
	gen.ips = ips
	gen.masks = masks

	return gen, nil
}

// RandomWeightedV6Generator builds a generator with weighted subnets intended
// to approximate real-world traffic. Given a count argument, the function
// will instantiate random subnets of the following weights:
// * 30% of traffic from a subnet of size /112 to /124
// * 10% of traffic from between 2 and 5 subnets of size /104 to /124
// * All remaining traffic from random subnets between /64 and /124
// Specifying a higher count will result in a greater number in the final category.
func RandomWeightedV6Generator(count int) (*V6Gen, error) {
	var subnets []*net.IPNet

	// First generate a small subnet that will make most of our IPs
	netstr := rd.IpV6Address() + fmt.Sprintf("/%d", 112+rand.Intn(12))
	_, n, err := net.ParseCIDR(netstr)
	if err != nil {
		return nil, err
	}
	// Now put that subnet in several times
	for i := 0; i < int(3*count/10); i++ {
		subnets = append(subnets, n)
	}

	// Now generate a few more that will be pretty frequent
	for i := 0; i < 2+rand.Intn(3); i++ {
		netstr = rd.IpV6Address() + fmt.Sprintf("/%d", 104+rand.Intn(20))
		_, n, err = net.ParseCIDR(netstr)
		if err != nil {
			return nil, err
		}
		// Now put that subnet in several times
		for j := 0; j < int(count/10); j++ {
			subnets = append(subnets, n)
		}
	}

	// Now add some more one-offs
	for len(subnets) < count {
		netstr = rd.IpV6Address() + fmt.Sprintf("/%d", 64+rand.Intn(60))
		_, n, err = net.ParseCIDR(netstr)
		if err != nil {
			return nil, err
		}
		subnets = append(subnets, n)
	}
	return NewV6Generator(subnets)
}

// IP generates an IPv6 address based on the parameters of the generator
func (g *V6Gen) IP() net.IP {
	// First get a completely random IP
	ip := make(net.IP, 16)
	idx := rand.Intn(len(g.subnets))

	for i := range ip {
		ip[i] = byte(rand.Intn(255))
		ip[i] &= g.masks[idx][i]
		ip[i] |= g.ips[idx][i]
	}
	return ip
}
