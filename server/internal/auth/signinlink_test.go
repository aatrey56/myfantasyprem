package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSignInLink(t *testing.T) {
	impls(t, func(t *testing.T, s Service, c *clock) {
		_, u, err := s.CreateAccount(ctx, "Sam")
		if err != nil {
			t.Fatal(err)
		}
		link, err := s.IssueSignInLink(ctx, u.ID, "lg_x", u.ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(link.Code) < 40 || link.LeagueID != "lg_x" || !link.ExpiresAt.Equal(c.t.Add(SignInLinkTTL)) {
			t.Fatalf("link = %+v", link)
		}
		c.t = c.t.Add(SignInLinkTTL - time.Minute)
		tok, got, league, err := s.UseSignInLink(ctx, link.Code, allowGrant)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != u.ID || league != "lg_x" {
			t.Fatalf("signed in %s to %s", got.ID, league)
		}
		if uid, _, err := s.Authenticate(ctx, tok.AccessToken); err != nil || uid != u.ID {
			t.Fatalf("authenticate = %s, %v", uid, err)
		}
		// Single use.
		if _, _, _, err := s.UseSignInLink(ctx, link.Code, allowGrant); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("second use: %v", err)
		}
		if _, _, _, err := s.UseSignInLink(ctx, "nope", allowGrant); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("unknown: %v", err)
		}
	})
}

func TestSignInLinkExpires(t *testing.T) {
	impls(t, func(t *testing.T, s Service, c *clock) {
		_, u, _ := s.CreateAccount(ctx, "Sam")
		link := must[SignInLink](t)(s.IssueSignInLink(ctx, u.ID, "lg_x", u.ID))
		c.t = c.t.Add(SignInLinkTTL)
		if _, _, _, err := s.UseSignInLink(ctx, link.Code, allowGrant); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("expired: %v", err)
		}
	})
}

func TestSignInLinkRevokesOlder(t *testing.T) {
	impls(t, func(t *testing.T, s Service, c *clock) {
		_, u, _ := s.CreateAccount(ctx, "Sam")
		_, other, _ := s.CreateAccount(ctx, "Kit")
		old := must[SignInLink](t)(s.IssueSignInLink(ctx, u.ID, "lg_x", u.ID))
		others := must[SignInLink](t)(s.IssueSignInLink(ctx, other.ID, "lg_x", u.ID))
		fresh := must[SignInLink](t)(s.IssueSignInLink(ctx, u.ID, "lg_y", u.ID))
		if _, _, _, err := s.UseSignInLink(ctx, old.Code, allowGrant); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("older link: %v", err)
		}
		if _, _, l, err := s.UseSignInLink(ctx, fresh.Code, allowGrant); err != nil || l != "lg_y" {
			t.Fatalf("new link: %s %v", l, err)
		}
		// Another user's link is untouched.
		if _, got, _, err := s.UseSignInLink(ctx, others.Code, allowGrant); err != nil || got.ID != other.ID {
			t.Fatalf("other user's link: %v", err)
		}
	})
}

func TestSignInLinkUnknownOrDeletedUser(t *testing.T) {
	impls(t, func(t *testing.T, s Service, c *clock) {
		if _, err := s.IssueSignInLink(ctx, "usr_01hzzzzzzzzzzzzzzzzzzzzzzz", "lg_x", "usr_01hzzzzzzzzzzzzzzzzzzzzzzz"); !errors.Is(err, ErrNoUser) {
			t.Fatalf("unknown user: %v", err)
		}
		_, u, _ := s.CreateAccount(ctx, "Sam")
		link := must[SignInLink](t)(s.IssueSignInLink(ctx, u.ID, "lg_x", u.ID))
		if err := s.DeleteUser(ctx, u.ID); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := s.UseSignInLink(ctx, link.Code, allowGrant); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("deleted user: %v", err)
		}
	})
}

func TestSignInLinkPrune(t *testing.T) {
	impls(t, func(t *testing.T, s Service, c *clock) {
		_, u, _ := s.CreateAccount(ctx, "Sam")
		must[SignInLink](t)(s.IssueSignInLink(ctx, u.ID, "lg_x", u.ID))
		c.t = c.t.Add(SignInLinkTTL)
		if n, err := s.Prune(ctx); err != nil || n < 1 {
			t.Fatalf("prune = %d, %v", n, err)
		}
	})
}

// allowGrant accepts every sign-in link grant.
func allowGrant(context.Context, SignInLinkGrant) error { return nil }
