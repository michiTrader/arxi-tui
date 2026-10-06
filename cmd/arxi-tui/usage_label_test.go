package main

import "testing"

func TestHumanTokens(t *testing.T) {
	cases := map[uint64]string{
		0: "0", 850: "850", 999: "999", 1000: "1k", 1234: "1.2k",
		9999: "10k", 10_000: "10k", 15_400: "15k", 999_999: "1M",
		1_000_000: "1M", 1_300_000: "1.3M",
	}
	for n, want := range cases {
		if got := humanTokens(n); got != want {
			t.Errorf("humanTokens(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestUsageLabel(t *testing.T) {
	if got := usageLabel(0, 0, 0); got != "" {
		t.Errorf("fresh screen label = %q, want empty", got)
	}
	if got, want := usageLabel(1200, 3400, 800), "ctx 1.2k · ↑3.4k ↓800"; got != want {
		t.Errorf("usageLabel = %q, want %q", got, want)
	}
}
