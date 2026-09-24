package main

import (
	"net/http/httptest"
	"testing"
)

func TestGetTag(t *testing.T) {
	t.Setenv("HELLO_TAG", "env-tag")

	t.Run("falls back to HELLO_TAG when no query param is set", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		if got := GetTag(r); got != "env-tag" {
			t.Errorf("GetTag() = %q, want %q", got, "env-tag")
		}
	})

	t.Run("query param overrides HELLO_TAG", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/?tag=url-tag", nil)
		if got := GetTag(r); got != "url-tag" {
			t.Errorf("GetTag() = %q, want %q", got, "url-tag")
		}
	})

	t.Run("empty query param still falls back to HELLO_TAG", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/?tag=", nil)
		if got := GetTag(r); got != "env-tag" {
			t.Errorf("GetTag() = %q, want %q", got, "env-tag")
		}
	})
}

func TestGetIPFromRequest(t *testing.T) {
	t.Run("prefers X-Forwarded-For over RemoteAddr", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("x-forwarded-for", "203.0.113.5")
		r.RemoteAddr = "10.0.0.1:12345"
		if got := GetIPFromRequest(r); got != "203.0.113.5" {
			t.Errorf("GetIPFromRequest() = %q, want %q", got, "203.0.113.5")
		}
	})

	t.Run("falls back to RemoteAddr when no header is set", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = "10.0.0.1:12345"
		if got := GetIPFromRequest(r); got != "10.0.0.1:12345" {
			t.Errorf("GetIPFromRequest() = %q, want %q", got, "10.0.0.1:12345")
		}
	})
}

func TestHelloServer(t *testing.T) {
	t.Setenv("HOSTNAME", "test-pod")
	t.Setenv("HELLO_TAG", "env-tag")

	r := httptest.NewRequest("GET", "/?tag=url-tag", nil)
	w := httptest.NewRecorder()
	HelloServer(w, r)

	want := "Hello, " + r.RemoteAddr + "! I'm test-pod, running tag url-tag\n"
	if got := w.Body.String(); got != want {
		t.Errorf("HelloServer() body = %q, want %q", got, want)
	}
}
