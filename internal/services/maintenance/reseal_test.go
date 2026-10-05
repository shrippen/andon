package maintenance_test

import (
	"database/sql"
	"encoding/base64"
	"path/filepath"
	"testing"
	"time"

	"andon/internal/crypto"
	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/repos/data"
	"andon/internal/repos/misc"
	"andon/internal/repos/users"
	"andon/internal/services/maintenance"
)

// sealed is one encrypted value the fixture stores and where it sits.
type sealed struct {
	name    string
	purpose crypto.Purpose
	read    func(q db.Queryer) []byte
}

// seal encrypts text under the process master key.
func seal(t *testing.T, text string, purpose crypto.Purpose) []byte {
	t.Helper()
	blob, err := crypto.Encrypt(text, purpose, nil)
	if err != nil {
		t.Fatal(err)
	}
	return blob
}

// sealText is seal as base64, the form JSON configs keep.
func sealText(t *testing.T, text string, purpose crypto.Purpose) string {
	return base64.StdEncoding.EncodeToString(seal(t, text, purpose))
}

// unbase is a base64 value of a JSON map, nil when missing.
func unbase(v any) []byte {
	s, _ := v.(string)
	blob, _ := base64.StdEncoding.DecodeString(s)
	return blob
}

// storeEverything puts one secret in every place the app encrypts one,
// each holding the text "secret".
func storeEverything(t *testing.T, d *sql.DB) []sealed {
	t.Helper()
	sp := &model.Space{Kind: enums.SpacePersonal, Name: "x", Version: 1}
	if err := content.AddSpace(d, sp); err != nil {
		t.Fatal(err)
	}
	u := &model.User{Email: "a@b.c", Name: "a", Role: enums.RoleAdmin, IsActive: true, Locale: enums.LocaleDE,
		ColorMode: enums.ColorAuto, CreatedAt: time.Now().UTC()}
	if err := users.Add(d, u); err != nil {
		t.Fatal(err)
	}
	conn := &model.Connection{SpaceID: sp.ID, Key: "gitea", Name: "Gitea", Service: "gitea", URL: "https://git.example",
		CredentialMode: enums.CredentialShared, SecretEnc: seal(t, "secret", crypto.PurposeCredential), VerifyTLS: true, CreatedAt: time.Now().UTC()}
	if err := content.AddConnection(d, conn); err != nil {
		t.Fatal(err)
	}
	w := &model.Widget{SpaceID: sp.ID, Key: "api", Type: "custom_api", Title: "API",
		Config: map[string]any{"url": "https://api.example", "api_key_enc": sealText(t, "secret", crypto.PurposeCredential)}, Version: 1, UpdatedAt: time.Now().UTC()}
	if err := content.AddWidget(d, w); err != nil {
		t.Fatal(err)
	}
	steps := []error{
		content.SetCredential(d, conn.ID, model.UserHolder(u.ID), seal(t, "secret", crypto.PurposeCredential), 1, nil),
		content.SetOAuthClient(d, conn.ID, seal(t, "secret", crypto.PurposeCredential)),
		content.SetGrant(d, conn.ID, u.ID, seal(t, "secret", crypto.PurposeCredential)),
		users.UpdateTOTPSecret(d, u.ID, seal(t, "secret", crypto.PurposeTOTP)),
		data.AddChannel(d, &model.NotifyChannel{UserID: u.ID, Name: "ntfy", URLEnc: seal(t, "secret", crypto.PurposeNotify), Enabled: true}),
		content.AddRevision(d, &model.Revision{Kind: enums.RevisionWidget, EntityID: w.ID, SpaceID: sp.ID, Version: 1, CreatedAt: time.Now().UTC(),
			Data: map[string]any{"config": map[string]any{"api_key_enc": sealText(t, "secret", crypto.PurposeCredential)}}}),
		misc.SetSetting(d, "oidc", map[string]any{"secret_enc": sealText(t, "secret", crypto.PurposeSetting)}),
	}
	for _, err := range steps {
		if err != nil {
			t.Fatal(err)
		}
	}

	return []sealed{
		{"connection secret", crypto.PurposeCredential, func(q db.Queryer) []byte { c, _ := content.Connection(q, conn.ID); return c.SecretEnc }},
		{"personal credential", crypto.PurposeCredential, func(q db.Queryer) []byte {
			c, _ := content.Credential(q, conn.ID, model.UserHolder(u.ID))
			return c.SecretEnc
		}},
		{"oauth client", crypto.PurposeCredential, func(q db.Queryer) []byte { b, _ := content.OAuthClient(q, conn.ID); return b }},
		{"oauth grant", crypto.PurposeCredential, func(q db.Queryer) []byte { b, _ := content.Grant(q, conn.ID, u.ID); return b }},
		{"totp", crypto.PurposeTOTP, func(q db.Queryer) []byte { x, _ := users.Get(q, u.ID); return x.TOTPSecretEnc }},
		{"notify channel", crypto.PurposeNotify, func(q db.Queryer) []byte { l, _ := data.Channels(q, u.ID); return l[0].URLEnc }},
		{"tile secret", crypto.PurposeCredential, func(q db.Queryer) []byte { x, _ := content.Widget(q, w.ID); return unbase(x.Config["api_key_enc"]) }},
		{"tile revision", crypto.PurposeCredential, func(q db.Queryer) []byte {
			revs, _ := content.Revisions(q, enums.RevisionWidget, w.ID)
			cfg, _ := revs[0].Data["config"].(map[string]any)
			return unbase(cfg["api_key_enc"])
		}},
		{"oidc secret", crypto.PurposeSetting, func(q db.Queryer) []byte { s, _ := misc.Setting(q, "oidc"); return unbase(s["secret_enc"]) }},
	}
}

// Rotating the master key re-encrypts every secret the app stores; one
// left behind would be unreadable under the new key.
func TestRotateKeyResealsEverything(t *testing.T) {
	d, path := openTestDB(t)
	places := storeEverything(t, d)

	if _, err := maintenance.RotateKey(d, path, newMasterKey); err != nil {
		t.Fatalf("rotate: %v", err)
	}

	expectReadable(t, openRotated(t, path), places)
}

// openRotated opens the copy RotateKey wrote, as the next start with the
// new key does, and makes the new key the process key.
func openRotated(t *testing.T, path string) *sql.DB {
	t.Helper()
	salt, err := db.ReadSalt(path)
	if err != nil {
		t.Fatal(err)
	}
	crypto.Init(crypto.Derive(newMasterKey, salt))
	fileKey, err := crypto.DatabaseKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	copyDB, err := db.Open(filepath.Clean(path+".rekeyed"), fileKey)
	if err != nil {
		t.Fatalf("open rotated copy: %v", err)
	}
	t.Cleanup(func() { copyDB.Close() })
	return copyDB
}

// Rotation writes only the copy: restarting with the old key (the
// operator did not swap the secret yet) still reads every secret.
func TestRotateKeyLeavesLiveIntact(t *testing.T) {
	d, path := openTestDB(t)
	places := storeEverything(t, d)

	if _, err := maintenance.RotateKey(d, path, newMasterKey); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	expectReadable(t, d, places)
}
