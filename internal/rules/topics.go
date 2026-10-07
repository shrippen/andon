package rules

// Topics group rules across services for overview widgets:
//
//	updates   immich.update, komodo.updates, … → "Update-Zentrale"
//	backups   borg.backup_old, pgbackweb.failed, … → "Backup-Übersicht"

// Topic names a group of rules.
type Topic string

const (
	TopicNone    Topic = ""
	TopicUpdates Topic = "updates"
	TopicBackups Topic = "backups"
)

var topicRules = map[Topic][]string{
	TopicUpdates: {"immich.update", "authentik.update", "borg.update", "hass.updates", "truenas.app_updates",
		"komodo.updates", "nextcloud.app_updates", "proxmox.updates", "pangolin.newt_update",
		"wud.updates", "watchtower.failed", "cross.update_unbacked", "cross.release_newer"},
	TopicBackups: {"borg.backup_old", "borg.jobs_failed", "borg.client_offline", "pgbackweb.failed", "pgbackweb.stale",
		"pgbackweb.silent", "truenas.snapshot_failed", "backups.gap",
		"backups.job", "pbs.verify_failed", "pbs.datastore_full", "cross.pbs_orphan", "cross.heartbeat_backup"},
}

// RulesOf returns the rule ids of a topic; nil for TopicNone.
func RulesOf(topic Topic) []string {
	return topicRules[topic]
}
