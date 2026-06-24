package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSetAndReadSessionCookie(t *testing.T) {
	w := httptest.NewRecorder()
	expires := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	SetSessionCookie(w, "abc123", expires)

	resp := w.Result()
	cookies := resp.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("len(cookies) = %d, want 1", len(cookies))
	}
	c := cookies[0]
	if c.Name != SessionCookieName {
		t.Errorf("name = %q, want %q", c.Name, SessionCookieName)
	}
	if c.Name != "__Host-houfeng_session" {
		t.Errorf("name = %q, want __Host-houfeng_session", c.Name)
	}
	if c.Value != "abc123" {
		t.Errorf("value = %q, want abc123", c.Value)
	}
	if !c.HttpOnly {
		t.Error("cookie must be HttpOnly")
	}
	if !c.Secure {
		t.Error("cookie must be Secure")
	}
	if c.SameSite != http.SameSiteStrictMode {
		t.Errorf("sameSite = %v, want Strict", c.SameSite)
	}
	if c.Path != "/" {
		t.Errorf("path = %q, want /", c.Path)
	}
}

func TestReadSessionCookieEmptyWhenAbsent(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	got := ReadSessionCookie(r)
	if got != "" {
		t.Errorf("ReadSessionCookie absent = %q, want empty", got)
	}
}

func TestReadSessionCookieValue(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "abc123"})
	got := ReadSessionCookie(r)
	if got != "abc123" {
		t.Errorf("ReadSessionCookie = %q, want abc123", got)
	}
}

func TestClearSessionCookie(t *testing.T) {
	w := httptest.NewRecorder()
	ClearSessionCookie(w)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("len(cookies) = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Value != "" {
		t.Errorf("value = %q, want empty", cookie.Value)
	}
	if cookie.MaxAge != -1 {
		t.Errorf("MaxAge = %d, want -1", cookie.MaxAge)
	}
	if !cookie.HttpOnly {
		t.Error("cleared cookie must be HttpOnly")
	}
	if !cookie.Secure {
		t.Error("cleared cookie must be Secure")
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("sameSite = %v, want Strict", cookie.SameSite)
	}
	if cookie.Path != "/" {
		t.Errorf("path = %q, want /", cookie.Path)
	}
}
