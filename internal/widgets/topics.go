package widgets

import "andon/internal/enums"

// Topic groups widget types by subject in the gallery ("Kachel hinzufügen").
// Unlike Category, which says how a type gets its data, a topic says what
// it is about.
type Topic string

const (
	TopicOverview Topic = "overview"
	TopicWork     Topic = "work"
	TopicAnalysis Topic = "analysis"
	TopicHomelab  Topic = "homelab"
	TopicNetwork  Topic = "network"
	TopicSecurity Topic = "security"
	TopicMedia    Topic = "media"
	TopicHome     Topic = "home"
	TopicWorld    Topic = "world"
	TopicDev      Topic = "dev"
)

// Topics lists the topics in gallery order.
var Topics = []Topic{TopicOverview, TopicWork, TopicAnalysis, TopicHomelab, TopicNetwork,
	TopicSecurity, TopicMedia, TopicHome, TopicWorld, TopicDev}

// topicOf maps each type key to its topic; unlisted types land in overview.
var topicOf = map[string]Topic{
	"timeline_recent":   TopicOverview,
	"links_down":        TopicOverview,
	"hint_noise":        TopicOverview,
	"hint_trend":        TopicOverview,
	"status_light":      TopicOverview,
	"exposure":          TopicSecurity,
	"greeting":          TopicOverview,
	"hints":             TopicOverview,
	"week_story":        TopicOverview,
	"conn_health":       TopicOverview,
	"clock":             TopicOverview,
	"calendar":          TopicOverview,
	"note":              TopicOverview,
	"list":              TopicOverview,
	"link":              TopicOverview,
	"deadlines":         TopicOverview,
	"expiries":          TopicOverview,
	"kimai_week":        TopicWork,
	"kimai_split":       TopicWork,
	"unbilled_age":      TopicWork,
	"invoice_aging":     TopicWork,
	"mail_invoices":     TopicWork,
	"dawarich_day":      TopicWork,
	"paperless_inbox":   TopicWork,
	"kintsugi":          TopicWork,
	"kpi":               TopicAnalysis,
	"chart":             TopicAnalysis,
	"table":             TopicAnalysis,
	"trend":             TopicAnalysis,
	"progress":          TopicAnalysis,
	"jsonapi":           TopicAnalysis,
	"custom_api":        TopicAnalysis,
	"sysinfo":           TopicHomelab,
	"glances_chart":     TopicHomelab,
	"monitors":          TopicHomelab,
	"disks":             TopicHomelab,
	"truenas_pools":     TopicHomelab,
	"backups":           TopicHomelab,
	"storage_forecast":  TopicHomelab,
	"updates":           TopicHomelab,
	"update_window":     TopicHomelab,
	"homelab_cost":      TopicHomelab,
	"immich_library":    TopicHomelab,
	"docker_containers": TopicHomelab,
	"umami_sites":       TopicHomelab,
	"adguard":           TopicNetwork,
	"pihole":            TopicNetwork,
	"gateway":           TopicNetwork,
	"vpn":               TopicNetwork,
	"speedtest":         TopicNetwork,
	"speed_history":     TopicNetwork,
	"public_ip":         TopicNetwork,
	"authentik_logins":  TopicSecurity,
	"vaultwarden_2fa":   TopicSecurity,
	"expiry":            TopicSecurity,
	"sabnzbd":           TopicMedia,
	"freshrss_feeds":    TopicMedia,
	"linkwarden":        TopicMedia,
	"apod":              TopicMedia,
	"xkcd":              TopicMedia,
	"joke":              TopicMedia,
	"image":             TopicMedia,
	"hass":              TopicHome,
	"weather":           TopicHome,
	"transit":           TopicWorld,
	"flights":           TopicWorld,
	"holidays":          TopicWorld,
	"rates":             TopicWorld,
	"stocks":            TopicWorld,
	"crypto":            TopicWorld,
	"gitea_reviews":     TopicDev,
	"iframe":            TopicDev,
}

// TopicOf returns the gallery topic of a type key.
func TopicOf(key string) Topic {
	if topic := registry[key].Topic; topic != "" {
		return topic
	}
	if topic, ok := topicOf[key]; ok {
		return topic
	}
	return TopicOverview
}

// starterPick is the first tile a service gets on a suggested board where
// it offers several types.
var starterPick = map[enums.ServiceType]string{
	enums.ServiceKimai:        "kimai_week",
	enums.ServiceInvoiceNinja: "invoice_aging",
	enums.ServiceGlances:      "sysinfo",
	enums.ServiceSpeedtest:    "speedtest",
}

// noStarter lists service types that show nothing without setup first.
var noStarter = map[enums.ServiceType]bool{enums.ServiceJSONAPI: true}

// Starter returns the tile type a connection of service starts with, e.g.
// Kimai → kimai_week; false if the service has no tile of its own.
func Starter(service enums.ServiceType) (string, bool) {
	if noStarter[service] {
		return "", false
	}
	if key, ok := starterPick[service]; ok {
		return key, true
	}
	for _, kind := range AllTypes() {
		if kind.Service == service {
			return kind.Key, true
		}
	}
	return "", false
}
