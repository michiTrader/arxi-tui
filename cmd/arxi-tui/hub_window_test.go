package main

import "testing"

func TestParseWindowReadsHowPeopleWriteIt(t *testing.T) {
	ok := map[string]int{"": 0, "1000000": 1_000_000, "1M": 1_000_000, "1m": 1_000_000, "1.5M": 1_500_000, "200k": 200_000, "200K": 200_000, "1_000_000": 1_000_000, "1,000,000": 1_000_000}
	for in, want := range ok {
		got, err := parseWindow(in)
		if err != nil || got != want {
			t.Errorf("parseWindow(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"abc", "-5", "999", "500M", "1.0001k", "0"} {
		if _, err := parseWindow(bad); err == nil {
			t.Errorf("parseWindow(%q) should be refused", bad)
		}
	}
}

func fillModels(vals map[string]string) *hubForm {
	f := newModelsForm("p")
	for i := range f.fields {
		if v, ok := vals[f.fields[i].label]; ok {
			f.fields[i].value = v
		}
	}
	return f
}

func TestModelsFormCarriesCachePricesAndWindowToTheWork(t *testing.T) {
	w, msg := fillModels(map[string]string{
		"Model ids":                           "m1",
		"Price in (USD / 1M tokens)":          "3",
		"Price out (USD / 1M tokens)":         "15",
		"Cache read price (USD / 1M tokens)":  "0.3",
		"Cache write price (USD / 1M tokens)": "3.75",
		"Context window (tokens)":             "1M",
	}).submit()
	if msg != "" {
		t.Fatal(msg)
	}
	if w.CacheRead == nil || *w.CacheRead != 0.3 || w.CacheWrite == nil || *w.CacheWrite != 3.75 || w.Window != 1_000_000 {
		t.Fatalf("work lost the extras: %+v", w)
	}
}

func TestModelsFormRefusesACachePriceAloneAndABadWindow(t *testing.T) {
	if _, msg := fillModels(map[string]string{"Model ids": "m1", "Cache read price (USD / 1M tokens)": "0.3"}).submit(); msg == "" {
		t.Fatal("a cache price without in/out must be refused")
	}
	if _, msg := fillModels(map[string]string{"Model ids": "m1", "Context window (tokens)": "lots"}).submit(); msg == "" {
		t.Fatal("a bad window must be refused")
	}
	w, msg := fillModels(map[string]string{"Model ids": "m1"}).submit()
	if msg != "" || w.Window != 0 || w.CacheRead != nil {
		t.Fatalf("empty extras must stay empty: %+v %q", w, msg)
	}
}
