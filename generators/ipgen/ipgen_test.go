/*************************************************************************
 * Copyright 2019 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

package ipgen

import (
	"net"
	"testing"
)

func TestV4Gen(t *testing.T) {
	_, n, err := net.ParseCIDR("192.168.0.0/16")
	if err != nil {
		t.Fatal(err)
	}

	gen, err := NewV4Generator([]*net.IPNet{n})
	if err != nil {
		t.Fatal(err)
	}

	ip := gen.IP()

	if !n.Contains(ip) {
		t.Fatalf("generator produced ip %v that is not in subnet %v\n", ip, n)
	}
}

func TestV6Gen(t *testing.T) {
	_, n, err := net.ParseCIDR("dead::/64")
	if err != nil {
		t.Fatal(err)
	}

	gen, err := NewV6Generator([]*net.IPNet{n})
	if err != nil {
		t.Fatal(err)
	}

	ip := gen.IP()

	if !n.Contains(ip) {
		t.Fatalf("generator produced ip %v that is not in subnet %v\n", ip, n)
	}
}

func TestRandomWeightedV4Generator(t *testing.T) {
	gen, err := RandomWeightedV4Generator(20)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 100; i++ {
		_ = gen.IP()
	}
}

func TestRandomWeightedV6Generator(t *testing.T) {
	gen, err := RandomWeightedV6Generator(20)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 100; i++ {
		gen.IP()
	}
}
