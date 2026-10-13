// mcp-memory-server-go - Personal Knowledge Graph MCP Server
// Copyright (C) 2026  Faiq Najib
//
// SPDX-License-Identifier: GPL-2.0-only

package usecase

import "testing"

func TestNormalizeRelationType(t *testing.T) {
	cases := []struct{ in, want string }{
		{"deployed via", "DEPLOYED_VIA"},
		{"DEPLOYED_VIA", "DEPLOYED_VIA"},
		{"  deployed-via  ", "DEPLOYED_VIA"},
		{"uses", "USES"},
		{"a--b", "A_B"},
		{"a   b", "A_B"}, // runs of separators collapse to a single underscore
		{"lower case words", "LOWER_CASE_WORDS"},
		{"", ""},
		{"   ", ""},
		{"---", ""},
		{"_leading", "LEADING"}, // leading separators are trimmed
	}
	for _, c := range cases {
		if got := normalizeRelationType(c.in); got != c.want {
			t.Errorf("normalizeRelationType(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeEntityType(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Project", "project"},
		{"  Person ", "person"},
		{"PROJECT", "project"},
		{"", "concept"},
		{"   ", "concept"},
	}
	for _, c := range cases {
		if got := normalizeEntityType(c.in); got != c.want {
			t.Errorf("normalizeEntityType(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDefaultProject(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "default"},
		{"   ", "default"},
		{"mis-apar", "mis-apar"},
		{"  mis-apar ", "mis-apar"},
	}
	for _, c := range cases {
		if got := defaultProject(c.in); got != c.want {
			t.Errorf("defaultProject(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
