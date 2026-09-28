package widgetlib

import (
	"context"
	"database/sql"
	"time"

	"andon/internal/enums"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/linkstatus"
	"andon/internal/services/svcdata"
	"andon/internal/services/util"
	"andon/internal/widgets"
)

// linkType is the link tile's widget type; statusSource its check.
const (
	linkType     = "link"
	statusSource = "http_status"
)

// linksDown lists the link tiles of a space the viewer may see whose
// last status check failed, with the first day they gave no answer.
// It reads the stored checks only: rendering never probes a site.
func linksDown(ctx context.Context, d *sql.DB, who *access.Principal, spaceID int64) ([]widgets.DownLink, error) {
	all, err := content.Widgets(d, []int64{spaceID})
	if err != nil {
		return nil, err
	}
	kind, _ := widgets.Get(linkType)
	today := time.Now().UTC()
	var out []widgets.DownLink
	for _, w := range all {
		if w.Type != linkType {
			continue
		}
		if granted, err := widgetRight(d, who, w); err != nil || granted < enums.RightView {
			continue
		}
		cfg, _ := widgets.Decode(w.Type, util.OpenSecrets(w.Config))
		for _, q := range kind.Queries(cfg) {
			if q.Source != statusSource {
				continue
			}
			res, err := svcdata.Get(ctx, d, statusSource, q.Params, nil, nil, svcdata.Stored)
			status, ok := res.Data.(interface{ Outcome() (bool, int) })
			if err != nil || !ok {
				continue
			}
			if up, _ := status.Outcome(); up {
				continue
			}
			link, _ := cfg.(widgets.LinkConfig)
			down := widgets.DownLink{Title: w.Title, URL: link.URL}
			if days := linkstatus.DownDays(d, w.ID, today); days > 0 {
				down.Since = today.AddDate(0, 0, 1-days)
			}
			out = append(out, down)
		}
	}
	return out, nil
}
