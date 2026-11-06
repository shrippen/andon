package vatcheck

import (
	"database/sql"

	"andon/internal/bayes"
	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/services/access"
	"andon/internal/services/learn"
	"andon/internal/sources"
)

// The VAT check's models (metrics.BankLearning), learned from each
// Invoice Ninja's bookings in the background job (services/learn).
func init() {
	for key, teach := range map[string]func(*bayes.Model, *sources.NinjaDataset) bool{
		metrics.PurposeVendor:   metrics.LearnVendors,
		metrics.PurposePrivate:  metrics.LearnPrivate,
		metrics.PurposeCategory: metrics.LearnCategories,
		metrics.PurposeTax:      metrics.LearnTaxes,
		metrics.PurposeShare:    metrics.LearnShares,
		metrics.PurposeNote:     metrics.LearnNotes,
	} {
		learn.Register(learn.Purpose{Key: key, Service: enums.ServiceInvoiceNinja, Teach: ninjaTeach(teach)})
	}
}

// ninjaTeach adapts a lesson on an Invoice Ninja dataset to learn.Purpose.
func ninjaTeach(teach func(*bayes.Model, *sources.NinjaDataset) bool) func(*bayes.Model, any, *model.Connection) bool {
	return func(m *bayes.Model, data any, _ *model.Connection) bool {
		n, _ := data.(*sources.NinjaDataset)
		return n != nil && teach(m, n)
	}
}

// bankLearning reads the stored models as who uses them, each taught
// today's data too (in memory): a booking just linked counts at once.
func bankLearning(d *sql.DB, who *access.Principal, conn *model.Connection, n *sources.NinjaDataset) metrics.BankLearning {
	h := model.UserHolder(who.UserID)
	use := func(purpose string, teach func(*bayes.Model, *sources.NinjaDataset) bool) bayes.Use {
		u := learn.Model(d, purpose, conn, h)
		if u.On() {
			teach(u.Model, n)
		}
		return u
	}
	return metrics.BankLearning{
		Vendor:   use(metrics.PurposeVendor, metrics.LearnVendors),
		Private:  use(metrics.PurposePrivate, metrics.LearnPrivate),
		Category: use(metrics.PurposeCategory, metrics.LearnCategories),
		Tax:      use(metrics.PurposeTax, metrics.LearnTaxes),
		Share:    use(metrics.PurposeShare, metrics.LearnShares),
		Note:     use(metrics.PurposeNote, metrics.LearnNotes),
	}
}
