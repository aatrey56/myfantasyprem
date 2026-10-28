package auth

import (
	"slices"
	"testing"
)

func TestSignInWithIdentity(t *testing.T) {
	impls(t, func(t *testing.T, s Service, _ *clock) {
		// First Google sign-in creates the account with the verified email.
		g := Identity{Provider: "google", Subject: "g-123", Email: "Sam@Example.com", EmailVerified: true, Name: "Sam Parker"}
		tok, u, created, err := s.SignInWithIdentity(ctx, g)
		if err != nil || !created || u.Email != "sam@example.com" || u.DisplayName != "Sam Parker" || tok.AccessToken == "" {
			t.Fatalf("first sign-in = %+v %v %v", u, created, err)
		}
		// Again: the same account.
		_, again, created, err := s.SignInWithIdentity(ctx, g)
		if err != nil || created || again.ID != u.ID {
			t.Fatalf("second sign-in = %+v %v %v", again, created, err)
		}
		// Apple with the same verified email links to it.
		a := Identity{Provider: "apple", Subject: "a-456", Email: "sam@example.com", EmailVerified: true}
		_, linked, created, err := s.SignInWithIdentity(ctx, a)
		if err != nil || created || linked.ID != u.ID || !slices.Contains(linked.Providers, "apple") || !slices.Contains(linked.Providers, "google") {
			t.Fatalf("linked = %+v %v %v", linked, created, err)
		}
		// An unverified email never links: a new account without email.
		x := Identity{Provider: "apple", Subject: "a-789", Email: "sam@example.com", EmailVerified: false}
		_, other, created, err := s.SignInWithIdentity(ctx, x)
		if err != nil || !created || other.ID == u.ID || other.Email != "" || other.DisplayName != "sam" {
			t.Fatalf("unverified = %+v %v %v", other, created, err)
		}
		// The magic link for the same email finds the linked account.
		tk, _ := startEmail(t, s, "sam@example.com")
		_, byMail, _, err := s.VerifyEmail(ctx, tk)
		if err != nil || byMail.ID != u.ID {
			t.Fatalf("email sign-in = %+v %v", byMail, err)
		}
		// Deleting the account frees the identity.
		if err := s.DeleteUser(ctx, u.ID); err != nil {
			t.Fatal(err)
		}
		_, fresh, created, err := s.SignInWithIdentity(ctx, g)
		if err != nil || !created || fresh.ID == u.ID {
			t.Fatalf("after deletion = %+v %v %v", fresh, created, err)
		}
	})
}
