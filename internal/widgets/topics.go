package widgets

import (
	"cmp"

	"andon/internal/enums"
)

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

// TopicOf returns the gallery topic of a type key.
func TopicOf(key string) Topic {
	return cmp.Or(registry[key].Topic, TopicOverview)
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
