package api

import (
	"strings"
	"testing"
)

const cookiesTxtOK = "# Netscape HTTP Cookie File\n" +
	"# This is a generated file! Do not edit.\n" +
	"\n" +
	".kick.com\tTRUE\t/\tTRUE\t1781000000\tkick_session\tsess-val\n" +
	"#HttpOnly_.kick.com\tTRUE\t/\tTRUE\t1781000000\tsession_token\ttok-val%7Cabc\n" +
	".kick.com\tTRUE\t/\tTRUE\t1781000000\tXSRF-TOKEN\txsrf-val\n" +
	".kick.com\tTRUE\t/\tTRUE\t1781000000\tcf_clearance\tcf-val\n" +
	".example.com\tTRUE\t/\tTRUE\t1781000000\tkick_session\tWRONG-DOMAIN\n"

func TestKickCookiesFromNetscape_HappyPath(t *testing.T) {
	f, err := kickCookiesFromNetscape(cookiesTxtOK)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.KickSession != "sess-val" {
		t.Errorf("KickSession = %q, want sess-val (kick.com row, not example.com)", f.KickSession)
	}
	if f.SessionToken != "tok-val%7Cabc" {
		t.Errorf("SessionToken = %q (HttpOnly_ prefix must be tolerated)", f.SessionToken)
	}
	if f.XSRF != "xsrf-val" {
		t.Errorf("XSRF = %q, want xsrf-val", f.XSRF)
	}
	if f.CFClearance != "cf-val" {
		t.Errorf("CFClearance = %q, want cf-val", f.CFClearance)
	}
}

func TestKickCookiesFromNetscape_MissingRequired(t *testing.T) {
	// session_token absent → error must name it.
	raw := ".kick.com\tTRUE\t/\tTRUE\t1781000000\tkick_session\tsess\n"
	_, err := kickCookiesFromNetscape(raw)
	if err == nil || !strings.Contains(err.Error(), "session_token") {
		t.Fatalf("want error naming session_token, got %v", err)
	}
}

func TestKickCookiesFromNetscape_NoKickRows(t *testing.T) {
	// Plain and #HttpOnly_-prefixed foreign domains must both be filtered.
	raw := "# Netscape HTTP Cookie File\n.example.com\tTRUE\t/\tTRUE\t1\tfoo\tbar\n" +
		"#HttpOnly_.example.com\tTRUE\t/\tTRUE\t1\tkick_session\tWRONG\n"
	_, err := kickCookiesFromNetscape(raw)
	if err == nil || !strings.Contains(err.Error(), "kick.com") {
		t.Fatalf("want 'no kick.com cookies' error, got %v", err)
	}
}

func TestKickCookiesFromNetscape_GarbageAndCRLF(t *testing.T) {
	raw := "not a cookie line\r\n\r\n.kick.com\tTRUE\t/\tTRUE\t1\tkick_session\ts\r\n" +
		".kick.com\tTRUE\t/\tTRUE\t1\tsession_token\tt\r\n"
	f, err := kickCookiesFromNetscape(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.KickSession != "s" || f.SessionToken != "t" {
		t.Errorf("CRLF parse: got %+v", f)
	}
}
