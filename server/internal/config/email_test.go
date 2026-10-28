package config

import "testing"

func TestLoadEmail(t *testing.T) {
	for _, tc := range []struct {
		env  map[string]string
		want string
	}{
		{nil, EmailNone},
		{map[string]string{"AUTH_DEV_MAGIC_LINKS": "true"}, EmailLog},
		{map[string]string{"EMAIL_PROVIDER": "log"}, EmailLog},
		{map[string]string{"EMAIL_PROVIDER": "Resend", "RESEND_API_KEY": "re_x", "EMAIL_FROM": "a@b.test"}, EmailResend},
	} {
		cfg, err := Load(env(tc.env))
		if err != nil || cfg.EmailProvider != tc.want {
			t.Errorf("%v: got %q, %v; want %q", tc.env, cfg.EmailProvider, err, tc.want)
		}
	}
	for _, m := range []map[string]string{
		{"EMAIL_PROVIDER": "smtp"},
		{"EMAIL_PROVIDER": "resend"},
		{"EMAIL_PROVIDER": "resend", "RESEND_API_KEY": "re_x"},
	} {
		if _, err := Load(env(m)); err == nil {
			t.Errorf("want error for %v", m)
		}
	}
}
