/*************************************************************************
 * Copyright 2025 Gravwell, Inc. All rights reserved.
 * Contact: <legal@gravwell.io>
 *
 * This software may be modified and distributed under the terms of the
 * BSD 2-clause license. See the LICENSE file for details.
 **************************************************************************/

// Package tests contains auto-generated decoder tests for sFlow v5 datagrams.
//
// # Test Generation Process
//
// These tests are generated using a two-step process:
//
// 1. The testgen binary (sflow/internal/cmd/testgen) captures raw sFlow packets
// as .bin files by listening on UDP port 6343. Run it with:
//
//	go run sflow/internal/cmd/testgen/... -tests-dir <tests_dir>
//
// 2. The sflowtool-ref.sh script generates reference JSON output by running
// the official sflowtool (via Docker) on each .bin file:
//
//	bash sflow/internal/cmd/testgen/sflowtool-ref.sh <tests_dir>
//
// This creates a .json file for each .bin file containing sflowtool's decoded
// output, which serves as the reference implementation to validate against.
//
// # Test Structure
//
// Each test consists of three files:
//   - <name>.bin: Raw sFlow datagram bytes (embedded in the _test.go file)
//   - <name>.json: Reference output from sflowtool showing the expected decoded values
//   - <name>_test.go: Auto-generated Go test that decodes the .bin file and compares
//     the result against the expected datagram structure
//
// The _test.go files contain a manually-constructed expected datagram structure
// that should match what sflowtool outputs in the .json file. Tests validate that
// the Go decoder produces the same values as the canonical sflowtool implementation.
package tests
