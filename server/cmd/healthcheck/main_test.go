package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTarget(t *testing.T) {
	for _, tc := range []struct {
		args []string
		addr string
		want string
	}{
		{nil, "", "http://127.0.0.1:8080/healthz"},
		{nil, ":9000", "http://127.0.0.1:9000/healthz"},
		{nil, "0.0.0.0:8081", "http://127.0.0.1:8081/healthz"},
		{[]string{"http://x:1/healthz"}, ":9000", "http://x:1/healthz"},
	} {
		if got := target(tc.args, tc.addr); got != tc.want {
			t.Errorf("target(%v, %q) = %q, want %q", tc.args, tc.addr, got, tc.want)
		}
	}
}

func TestCheck(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer ok.Close()
	if err := check(ok.URL); err != nil {
		t.Fatal(err)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) }))
	defer bad.Close()
	if err := check(bad.URL); err == nil {
		t.Fatal("want error on 503")
	}
}
