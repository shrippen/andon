package rules

// Lemmy:
//
//	lemmy.replies   unread replies to and mentions of the account
//
// Its subscribed posts count in cross.project_mentioned, its own posts
// in cross.release_unannounced.

import (
	"andon/internal/enums"
	"andon/internal/sources"
)

var lemmySvc = string(enums.ServiceLemmy)

func init() {
	Register("lemmy.replies", lemmySvc, nil, on(lemmyReplies))
}

func lemmyReplies(data *sources.LemmyDataset, _ map[string]any, _ Env) []Finding {
	if len(data.Replies) == 0 {
		return nil
	}
	first := data.Replies[0]
	return []Finding{svcFinding(lemmySvc, "lemmy.replies", "unread", "lemmy.replies", enums.SeverityInfo, data.URL+"/inbox",
		map[string]any{"count": len(data.Replies), "from": first.From, "text": orDash(first.Text)})}
}
