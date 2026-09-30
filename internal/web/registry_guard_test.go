package web

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/i18n"
	"andon/internal/rules"
	"andon/internal/sources"
	"andon/internal/widgets"
)

// Guards for adding a tile, rule or service: what a new one needs and
// would otherwise only fail at runtime (a raw key, an empty tile).

// TestEveryTileHasTemplateAndName: a template define and a name and
// description in both languages.
func TestEveryTileHasTemplateAndName(t *testing.T) {
	for _, kind := range widgets.AllTypes() {
		if templates.Lookup(kind.Template) == nil {
			t.Errorf("%s: no template %q", kind.Key, kind.Template)
		}
		for _, key := range []string{"wtype." + kind.Key + ".name", "wtype." + kind.Key + ".desc"} {
			checkBoth(t, key)
		}
	}
}

// TestTopicRulesExist: a topic listing an unknown rule id would silently
// drop that rule from its overview tile.
func TestTopicRulesExist(t *testing.T) {
	known := map[string]bool{}
	for _, spec := range rules.AllRules() {
		known[spec.ID] = true
	}
	for _, topic := range []rules.Topic{rules.TopicUpdates, rules.TopicBackups} {
		for _, id := range rules.RulesOf(topic) {
			if !known[id] {
				t.Errorf("topic %s: unknown rule %q", topic, id)
			}
		}
	}
}

// ownServices are built into Andon or generic: no project to link.
var ownServices = map[enums.ServiceType]bool{enums.ServiceCerts: true, enums.ServiceMail: true, enums.ServiceDomains: true,
	enums.ServiceBlacklist: true, enums.ServiceGateway: true, enums.ServiceCalendar: true, enums.ServiceKintsugi: true,
	enums.ServiceJSONAPI: true}

// TestEveryServiceIsComplete: a connectable service has a dataset, a
// name and, unless listed in ownServices, a project link.
func TestEveryServiceIsComplete(t *testing.T) {
	for _, svc := range enums.Services {
		if _, err := sources.Get(sources.DataKey(svc)); err != nil {
			t.Errorf("%s: no dataset source %q", svc, sources.DataKey(svc))
		}
		checkBoth(t, "service."+string(svc))
		if projectURL(svc) == "" && !ownServices[svc] {
			t.Errorf("%s: no project link", svc)
		}
	}
}

func checkBoth(t *testing.T, key string) {
	t.Helper()
	for _, loc := range []enums.Locale{enums.LocaleDE, enums.LocaleEN} {
		if i18n.T(key, loc, nil) == key {
			t.Errorf("%s: missing %q", loc, key)
		}
	}
}
