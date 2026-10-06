package caps

import "andon/internal/enums"

// SignIn is how a service logs in without a pasted token.
type SignIn string

const (
	SignInNone     SignIn = ""
	SignInRedirect SignIn = "redirect" // browser goes to the service and back
	SignInLink     SignIn = "link"     // login link in a new tab, Andon polls
	SignInCode     SignIn = "code"     // code shown here, approved there, Andon polls
	SignInClient   SignIn = "client"   // registered client only, no browser step
)

// Traits are what a service offers besides shared domains.
type Traits struct {
	Webhooks    bool   // fed by webhooks instead of polling (hooks.Accepts)
	Restore     bool   // keeps backups that deserve a restore test
	SignIn      SignIn // at best; connect.MethodOf narrows it by address and options
	OAuthClient bool   // sign-in needs a registered OAuth client first
}

// traits in a fixed order: the restore tasks follow it.
var traits = []struct {
	service enums.ServiceType
	Traits
}{
	{enums.ServiceBorgBackup, Traits{Restore: true}},
	{enums.ServicePGBackWeb, Traits{Restore: true, Webhooks: true}},
	{enums.ServiceTrueNAS, Traits{Restore: true}},
	{enums.ServiceProxmox, Traits{Restore: true}},
	{enums.ServiceHomeAssistant, Traits{SignIn: SignInRedirect}},
	{enums.ServiceGitea, Traits{SignIn: SignInRedirect, OAuthClient: true}},
	{enums.ServiceSnipeIT, Traits{SignIn: SignInRedirect, OAuthClient: true}},
	{enums.ServiceNextcloud, Traits{SignIn: SignInLink}},
	{enums.ServiceMediaServer, Traits{SignIn: SignInCode}}, // Jellyfin only
	{enums.ServiceTailscale, Traits{SignIn: SignInClient, OAuthClient: true}},
}

// TraitsOf is a service's traits.
func TraitsOf(s enums.ServiceType) Traits {
	for _, t := range traits {
		if t.service == s {
			return t.Traits
		}
	}
	return Traits{}
}

// Restorable lists the services whose backups deserve a restore test.
func Restorable() []enums.ServiceType {
	var out []enums.ServiceType
	for _, t := range traits {
		if t.Restore {
			out = append(out, t.service)
		}
	}
	return out
}

// Keys names a service's traits for the record ("caps.trait.<key>").
func (t Traits) Keys() []string {
	var out []string
	if t.Webhooks {
		out = append(out, "webhooks")
	}
	if t.Restore {
		out = append(out, "restore")
	}
	if t.SignIn != SignInNone {
		out = append(out, "signin_"+string(t.SignIn))
	}
	if t.OAuthClient {
		out = append(out, "oauth_client")
	}
	return out
}
