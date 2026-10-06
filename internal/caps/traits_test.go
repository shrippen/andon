package caps

import (
	"slices"
	"testing"

	"andon/internal/enums"
)

// The declarations answer as the tables they replace did.
func TestTraits(t *testing.T) {
	want := []enums.ServiceType{enums.ServiceBorgBackup, enums.ServicePGBackWeb, enums.ServiceTrueNAS, enums.ServiceProxmox}
	if got := Restorable(); !slices.Equal(got, want) {
		t.Fatalf("restorable %v", got)
	}
	if !TraitsOf(enums.ServicePGBackWeb).Webhooks || TraitsOf(enums.ServiceBorgBackup).Webhooks {
		t.Fatal("webhooks")
	}
	for s, m := range map[enums.ServiceType]SignIn{enums.ServiceGitea: SignInRedirect, enums.ServiceNextcloud: SignInLink,
		enums.ServiceMediaServer: SignInCode, enums.ServiceTailscale: SignInClient, enums.ServiceKimai: SignInNone} {
		if got := TraitsOf(s).SignIn; got != m {
			t.Errorf("%s: %q", s, got)
		}
	}
	for _, s := range []enums.ServiceType{enums.ServiceGitea, enums.ServiceSnipeIT, enums.ServiceTailscale} {
		if !TraitsOf(s).OAuthClient {
			t.Errorf("%s needs a client", s)
		}
	}
	if keys := TraitsOf(enums.ServiceGitea).Keys(); !slices.Equal(keys, []string{"signin_redirect", "oauth_client"}) {
		t.Fatalf("keys %v", keys)
	}
}
