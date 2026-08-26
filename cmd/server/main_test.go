package main

import "testing"

func TestParseConfigDefaultsAndPort(t *testing.T) {
	cfg, err := parseConfig(nil, "")
	if err != nil || cfg.Address != "127.0.0.1:19081" {
		t.Fatalf("default: %#v %v", cfg, err)
	}
	cfg, err = parseConfig(nil, "19123")
	if err != nil || cfg.Address != "127.0.0.1:19123" {
		t.Fatalf("PORT: %#v %v", cfg, err)
	}
}

func TestParseConfigRejectsNonLoopback(t *testing.T) {
	if _, err := parseConfig([]string{"-addr=0.0.0.0:19081"}, ""); err == nil {
		t.Fatal("expected non-loopback rejection")
	}
	if _, err := parseConfig(nil, "invalid"); err == nil {
		t.Fatal("expected invalid PORT rejection")
	}
}
