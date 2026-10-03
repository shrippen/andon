package widgetlib

import (
	"context"
	"database/sql"
	"time"

	"andon/internal/model"
	"andon/internal/services/access"
)

// WatchIP exposes watchIP to the tests.
var WatchIP = watchIP

// DemoDetail loads a type's detail dialog from its demo datasets.
func DemoDetail(ctx context.Context, d *sql.DB, who *access.Principal, w *model.Widget) (*DetailDialog, error) {
	return loadTileDetail(ctx, d, who, w, "", time.Now().UTC(), originDemo)
}
