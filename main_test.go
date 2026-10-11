package main

import "testing"

func TestGreeting(t *testing.T) {
	if got := greeting(nil); got != "hello" {
		t.Fatalf("greeting(nil) = %q", got)
	}
	if got := greeting([]string{"org"}); got != "hello, org" {
		t.Fatalf("greeting(org) = %q", got)
	}
}
