package provider

import (
	"context"
	"testing"
)

func TestPtrOr(t *testing.T) {
	t.Run("int", func(t *testing.T) {
		var nilPtr *int
		if got := PtrOr(nilPtr, 42); got != 42 {
			t.Errorf("PtrOr(nil, 42) = %v, want 42", got)
		}
		five := 5
		if got := PtrOr(&five, 42); got != 5 {
			t.Errorf("PtrOr(&5, 42) = %v, want 5", got)
		}
	})
	t.Run("string", func(t *testing.T) {
		var nilPtr *string
		if got := PtrOr(nilPtr, "default"); got != "default" {
			t.Errorf("PtrOr(nil, \"default\") = %q, want \"default\"", got)
		}
		s := "hello"
		if got := PtrOr(&s, "default"); got != "hello" {
			t.Errorf("PtrOr(&\"hello\", \"default\") = %q, want \"hello\"", got)
		}
	})
	t.Run("bool", func(t *testing.T) {
		var nilPtr *bool
		if got := PtrOr(nilPtr, true); got != true {
			t.Errorf("PtrOr(nil, true) = %v, want true", got)
		}
		f := false
		if got := PtrOr(&f, true); got != false {
			t.Errorf("PtrOr(&false, true) = %v, want false", got)
		}
	})
}

func TestDexConfig_Configure_emptyHost(t *testing.T) {
	cfg := &DexConfig{Host: ""}
	ctx := context.Background()
	err := cfg.Configure(ctx)
	if err == nil {
		t.Fatal("Configure() with empty host: want error, got nil")
	}
	if err.Error() != "host is required" {
		t.Errorf("Configure() error = %q, want %q", err.Error(), "host is required")
	}
}
