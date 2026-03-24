// Copyright 2026 cloudygreybeard
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestDryRunMode(t *testing.T) {
	tests := []struct {
		name string
		val  string
		want string
	}{
		{"client", "client", DryRunClient},
		{"server", "server", DryRunServer},
		{"none", "none", DryRunNone},
		{"empty defaults to server", "", DryRunServer},
		{"bogus defaults to server", "bogus", DryRunServer},
		{"unset defaults to server", "", DryRunServer},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viper.Reset()
			if tt.val != "" {
				viper.Set("dry-run", tt.val)
			}
			if got := dryRunMode(); got != tt.want {
				t.Errorf("dryRunMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFilterRules(t *testing.T) {
	rules := []Rule{
		{Resource: "foo-service", SelfLink: "https://example.com/foo", Permission: "Reader"},
		{Resource: "bar-service", SelfLink: "https://example.com/bar", Permission: "Writer"},
		{Resource: "baz-service", SelfLink: "https://example.com/baz", Permission: "Reader"},
	}

	t.Run("no args returns all", func(t *testing.T) {
		got, err := filterRules(rules, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 3 {
			t.Errorf("got %d rules, want 3", len(got))
		}
	})

	t.Run("filter by resource ID", func(t *testing.T) {
		got, err := filterRules(rules, []string{"foo-service"})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Resource != "foo-service" {
			t.Errorf("got %+v, want foo-service only", got)
		}
	})

	t.Run("filter multiple resources", func(t *testing.T) {
		got, err := filterRules(rules, []string{"foo-service", "baz-service"})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Errorf("got %d rules, want 2", len(got))
		}
	})

	t.Run("case insensitive match", func(t *testing.T) {
		got, err := filterRules(rules, []string{"FOO-SERVICE"})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Errorf("got %d rules, want 1", len(got))
		}
	})

	t.Run("ad-hoc URL", func(t *testing.T) {
		got, err := filterRules(rules, []string{"https://portal.example.com/something"})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].SelfLink != "https://portal.example.com/something" {
			t.Errorf("got %+v, want ad-hoc URL rule", got)
		}
	})

	t.Run("mix of resource IDs and URLs", func(t *testing.T) {
		got, err := filterRules(rules, []string{"bar-service", "https://portal.example.com/ad-hoc"})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Errorf("got %d rules, want 2", len(got))
		}
		if got[0].Resource != "bar-service" {
			t.Errorf("first rule: got resource %q, want bar-service", got[0].Resource)
		}
		if got[1].SelfLink != "https://portal.example.com/ad-hoc" {
			t.Errorf("second rule: got selfLink %q, want ad-hoc URL", got[1].SelfLink)
		}
	})

	t.Run("unknown resource returns error", func(t *testing.T) {
		_, err := filterRules(rules, []string{"nonexistent"})
		if err == nil {
			t.Fatal("expected error for unknown resource")
		}
		if !strings.Contains(err.Error(), "nonexistent") {
			t.Errorf("error = %q, want mention of nonexistent", err)
		}
	})

	t.Run("partial unknown returns error", func(t *testing.T) {
		_, err := filterRules(rules, []string{"foo-service", "nonexistent"})
		if err == nil {
			t.Fatal("expected error when one resource is unknown")
		}
	})
}

func TestRequireGroup(t *testing.T) {
	t.Run("returns configured group", func(t *testing.T) {
		viper.Reset()
		viper.Set("group", "sre")
		got, err := requireGroup()
		if err != nil {
			t.Fatal(err)
		}
		if got != "sre" {
			t.Errorf("requireGroup() = %q, want %q", got, "sre")
		}
	})

	t.Run("errors when unconfigured", func(t *testing.T) {
		viper.Reset()
		_, err := requireGroup()
		if err == nil {
			t.Fatal("expected error when group is not set")
		}
		if !strings.Contains(err.Error(), "no group configured") {
			t.Errorf("error = %q, want guidance message", err)
		}
	})
}

func TestAllURLArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want bool
	}{
		{"empty", nil, false},
		{"single URL", []string{"https://example.com"}, true},
		{"multiple URLs", []string{"https://a.com", "http://b.com"}, true},
		{"resource ID", []string{"foo-service"}, false},
		{"mixed", []string{"https://a.com", "foo-service"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := allURLArgs(tt.args); got != tt.want {
				t.Errorf("allURLArgs(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}
