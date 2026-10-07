package web

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"andon/internal/enums"
	"andon/internal/services/connections"
)

// credShape says whether a service's credential is one token or two named
// parts. The drivers still take a single "a:b" secret (see
// internal/sources/mail.go, internal/drivers/services/{homelab,infra}.go);
// this only changes how the form collects it, so users don't have to build
// that string by hand.
type credShape string

const (
	credSingle    credShape = "single"
	credUserPass  credShape = "userpass"
	credKeySecret credShape = "keysecret"
	credNone      credShape = "none"     // no login at all: no token field, no mode
	credTokenID   credShape = "tokenid"  // token ID and secret, joined "id=secret" (Proxmox)
	credICal      credShape = "ical"     // one field, but it holds a private iCal address
	credPassword  credShape = "password" // one field, but a password, not a token (Pi-hole)
)

// defaultURLs are the fixed API addresses of hosted services, filled in
// when such a connection is set up.
var defaultURLs = map[enums.ServiceType]string{
	enums.ServiceGitHub:    "https://api.github.com",
	enums.ServiceKDEStore:  "https://api.kde-look.org",
	enums.ServiceNVD:       "https://services.nvd.nist.gov",
	enums.ServiceTibber:    "https://api.tibber.com/v1-beta/gql",
	enums.ServiceDWD:       "https://api.brightsky.dev",
	enums.ServiceTailscale: "https://api.tailscale.com",
}

func defaultURL(service enums.ServiceType) string {
	return defaultURLs[service]
}

func credShapeOf(service enums.ServiceType) credShape {
	switch service {
	case enums.ServiceFreshRSS, enums.ServiceMail, enums.ServiceAdGuard, enums.ServiceUmami, enums.ServiceNextcloud:
		return credUserPass
	case enums.ServiceKomodo, enums.ServiceGateway:
		return credKeySecret
	case enums.ServiceProxmox:
		return credTokenID
	case enums.ServiceCalendar:
		return credICal
	case enums.ServicePihole:
		return credPassword
	case enums.ServiceScrutiny, enums.ServiceDocker, enums.ServiceCerts, enums.ServiceDomains, enums.ServiceBlacklist,
		enums.ServiceDWD, enums.ServicePGBackWeb, enums.ServiceKDEStore, enums.ServiceHansei, enums.ServiceApcupsd:
		return credNone
	default:
		return credSingle
	}
}

// errCredIncomplete: only one of two required parts was filled in.
var errCredIncomplete = errors.New("conn.cred_incomplete")

// singleKeyToo lists two-part services that also take one key alone,
// entered with the first field empty (Umami Cloud API key, Nextcloud
// serverinfo token, pfSense and UniFi keys).
var singleKeyToo = map[enums.ServiceType]bool{
	enums.ServiceUmami: true, enums.ServiceNextcloud: true, enums.ServiceGateway: true,
}

// formSecret reads the connection secret from the form, joining a two-part
// credential's fields into the "a:b" shape the drivers expect. Both parts
// empty means "unchanged" (create: no secret), same as the single field.
func formSecret(r *http.Request, service enums.ServiceType) (string, error) {
	shape := credShapeOf(service)
	switch shape {
	case credNone:
		return "", nil
	case credPassword:
		return r.FormValue("secret"), nil
	case credICal:
		return icalSecret(r)
	case credSingle:
		// Tokens and URLs never hold edge whitespace; a pasted one often does.
		return strings.TrimSpace(r.FormValue("secret")), nil
	}

	// User names, IDs and keys are trimmed, a password is kept as typed.
	a, b := strings.TrimSpace(r.FormValue("secret_a")), r.FormValue("secret_b")
	if shape != credUserPass || a == "" {
		b = strings.TrimSpace(b)
	}
	switch {
	case a == "" && b == "":
		return "", nil
	case a == "" && singleKeyToo[service]:
		return b, nil
	case a == "" || b == "":
		return "", errCredIncomplete
	case shape == credTokenID:
		return a + "=" + b, nil
	}
	return a + ":" + b, nil
}

// icalSecret is a private feed address, a login for the calendar's own
// address, or a private address with login:
//
//	secret ""         · user anna · password pw  →  anna:pw
//	secret https://h/x · user anna · password pw  →  https://anna:pw@h/x
func icalSecret(r *http.Request) (string, error) {
	feed := strings.TrimSpace(r.FormValue("secret"))
	user, password := strings.TrimSpace(r.FormValue("secret_a")), r.FormValue("secret_b")
	if user == "" && password == "" {
		return feed, nil
	}
	if user == "" || password == "" {
		return "", errCredIncomplete
	}
	if feed == "" {
		return user + ":" + password, nil
	}

	return connections.FeedWithLogin(feed, user, password), nil
}

// setupField is a connection option the setup form asks for directly,
// because the service does not work without it.
type setupField struct {
	Key      string    // option key, form field "opt_<key>"
	Label    string    // catalog key
	Kind     fieldKind // how the form asks for it
	Choices  []string  // fieldSelect: values, labelled "conn.choice_<value>"
	Hint     string    // catalog key of a line under the field, "" = none
	Optional bool      // the service also works without it
}

// fieldKind is how the setup form asks for an option.
type fieldKind string

const (
	fieldText   fieldKind = "text"
	fieldPlace  fieldKind = "place"  // search by name, stores place, lat and lon
	fieldSelect fieldKind = "select" // one of Choices, the first is the default
	fieldIDs    fieldKind = "ids"    // numbers separated by commas, also from links: ".../p/2368175, 2368948"
)

// idDigits finds the id in a pasted link: the last run of digits.
var idDigits = regexp.MustCompile(`\d+`)

// formIDs reads a fieldIDs value: ".../p/2368175, 2368948" → ["2368175", "2368948"].
func formIDs(text string) []any {
	var out []any
	for _, part := range strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' }) {
		if found := idDigits.FindAllString(part, -1); len(found) > 0 {
			out = append(out, found[len(found)-1])
		}
	}
	return out
}

// setupFields are those options per service, e.g. Pangolin's organisation.
var setupFields = map[enums.ServiceType][]setupField{
	enums.ServicePangolin:  {{Key: "org", Label: "conn.pangolin_org", Kind: fieldText}},
	enums.ServiceDWD:       {{Key: "place", Label: "conn.place", Kind: fieldPlace}},
	enums.ServiceTibber:    {{Key: "place", Label: "conn.place", Kind: fieldPlace}},
	enums.ServiceSpeedtest: {{Key: "kind", Label: "conn.speed_tool", Kind: fieldSelect, Choices: []string{"tracker", "myspeed"}}},
	enums.ServiceGateway:   {{Key: "kind", Label: "conn.router_kind", Kind: fieldSelect, Choices: []string{"opnsense", "pfsense", "unifi", "openwrt"}}},
	enums.ServiceKDEStore: {
		{Key: "user", Label: "conn.kdestore_user", Kind: fieldText, Hint: "conn.kdestore_user_hint", Optional: true},
		{Key: "ids", Label: "conn.kdestore_ids", Kind: fieldIDs, Hint: "conn.kdestore_ids_hint", Optional: true},
	},
}

func setupFieldsOf(service enums.ServiceType) []setupField {
	return setupFields[service]
}

// formOptions merges the setup fields sent with the form into options; nil
// means "no change" (the service has none, or none were sent).
func formOptions(r *http.Request, service enums.ServiceType, options map[string]any) map[string]any {
	fields := setupFields[service]
	if len(fields) == 0 {
		return nil
	}
	out := map[string]any{}
	for k, v := range options {
		out[k] = v
	}
	changed := false
	for _, f := range fields {
		if f.Kind == fieldPlace {
			changed = formPlace(r, out) || changed
			continue
		}
		if _, sent := r.Form["opt_"+f.Key]; !sent {
			continue
		}
		changed = true
		v := strings.TrimSpace(r.FormValue("opt_" + f.Key))
		switch {
		case f.Kind == fieldIDs && len(formIDs(v)) > 0:
			out[f.Key] = formIDs(v)
		case f.Kind != fieldIDs && v != "":
			out[f.Key] = v
		default:
			delete(out, f.Key)
		}
	}
	if !changed {
		return nil
	}
	return out
}

// formPlace takes a picked place into options: its name and coordinates as
// numbers (a text "50,98" would read as 5098). It reports whether the form
// carried a place at all.
func formPlace(r *http.Request, options map[string]any) bool {
	if _, sent := r.Form["opt_lat"]; !sent {
		return false
	}
	lat, errLat := strconv.ParseFloat(strings.TrimSpace(r.FormValue("opt_lat")), 64)
	lon, errLon := strconv.ParseFloat(strings.TrimSpace(r.FormValue("opt_lon")), 64)
	if errLat != nil || errLon != nil {
		delete(options, "lat")
		delete(options, "lon")
		delete(options, "place")
		return true
	}
	options["lat"], options["lon"] = lat, lon
	options["place"] = strings.TrimSpace(r.FormValue("opt_place"))
	return true
}

// optText shows an option as form text: "Weimar", 50.9803; "" when unset.
func optText(options map[string]any, key string) string {
	switch v := options[key].(type) {
	case nil:
		return ""
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case []any:
		parts := make([]string, len(v))
		for i, item := range v {
			parts[i] = optText(map[string]any{key: item}, key)
		}
		return strings.Join(parts, ", ")
	default:
		return fmt.Sprint(v)
	}
}

// secretLabels name the single secret field where "API token" would be
// wrong, e.g. MySpeed takes a password.
var secretLabels = map[enums.ServiceType]string{
	enums.ServiceSpeedtest:  "conn.secret_speedtest",
	enums.ServiceWallos:     "conn.secret_wallos",
	enums.ServicePrometheus: "conn.secret_prometheus",
	enums.ServiceNVD:        "conn.secret_nvd",
	enums.ServicePBS:        "conn.secret_pbs",
	enums.ServiceKopia:      "conn.secret_userpass_opt",
	enums.ServiceBackrest:   "conn.secret_userpass_opt",
	enums.ServiceUrBackup:   "conn.secret_userpass_opt",
	enums.ServiceWUD:        "conn.secret_userpass_opt",
	enums.ServicePeaNUT:     "conn.secret_userpass_opt",
	enums.ServiceOpenDTU:    "conn.secret_userpass_opt",
	enums.ServiceEVCC:       "conn.secret_userpass_opt",
	enums.ServiceTraefik:    "conn.secret_userpass_opt",
	enums.ServiceCaddy:      "conn.secret_userpass_opt",
	enums.ServiceNPM:        "conn.secret_npm",
	enums.ServiceFritzBox:   "conn.secret_fritz",
	enums.ServiceDuplicati:  "conn.secret_duplicati",
}

func secretLabel(service enums.ServiceType) string {
	if key, ok := secretLabels[service]; ok {
		return key
	}
	return "field.token"
}
