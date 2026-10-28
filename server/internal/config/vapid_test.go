package config

import "testing"

func TestVAPIDAllOrNone(t *testing.T) {
	cfg, err := Load(env(map[string]string{}))
	if err != nil || cfg.VAPIDPublicKey != "" {
		t.Fatalf("none: %+v %v", cfg.VAPIDPublicKey, err)
	}
	cfg, err = Load(env(map[string]string{"VAPID_PUBLIC_KEY": "pub", "VAPID_PRIVATE_KEY": "priv", "VAPID_SUBJECT": "mailto:a@b.c"}))
	if err != nil || cfg.VAPIDPublicKey != "pub" || cfg.VAPIDPrivateKey != "priv" || cfg.VAPIDSubject != "mailto:a@b.c" {
		t.Fatalf("all: %+v %v", cfg, err)
	}
	if _, err := Load(env(map[string]string{"VAPID_PUBLIC_KEY": "pub"})); err == nil {
		t.Fatal("a public key alone was accepted")
	}
}
