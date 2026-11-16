package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/idp"
)

func TestAppConfigSignInMethods(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tc := range []struct {
		name  string
		setup func(*Server)
		want  SignInMethods
	}{
		{"nothing configured", func(*Server) {}, SignInMethods{}},
		{"email logged in development", func(s *Server) { s.sender = auth.LogSender{Log: logger} }, SignInMethods{Email: true}},
		{"email by Resend", func(s *Server) { s.sender = auth.ResendSender{Log: logger} }, SignInMethods{Email: true}},
		{"email disabled", func(s *Server) { s.sender = auth.DisabledSender{Log: logger} }, SignInMethods{}},
		{"apple", func(s *Server) { s.apple = idp.Apple([]string{"com.example.app"}, nil) }, SignInMethods{Apple: true}},
		{"google", func(s *Server) { s.google = idp.Google([]string{"web.example"}, nil) }, SignInMethods{Google: true}},
		{"apple with no ids", func(s *Server) { s.apple = idp.Apple(nil, nil) }, SignInMethods{}},
		{"claim link", func(s *Server) { s.claimSignIn = true }, SignInMethods{ClaimLink: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := newTestServer()
			tc.setup(srv)
			h := newHandler(testOptions(), newRoutes(testOptions(), srv))
			rec := doAnon(h, http.MethodGet, "/v1/config", "")
			var cfg AppConfigResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil || rec.Code != 200 {
				t.Fatalf("config: %d %s %v", rec.Code, rec.Body, err)
			}
			if cfg.Data.SignIn != tc.want {
				t.Fatalf("signIn = %+v, want %+v", cfg.Data.SignIn, tc.want)
			}
		})
	}
}
