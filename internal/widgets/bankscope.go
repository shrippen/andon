package widgets

// Tiles over bank data choose the accounts they read: business, private
// or both (Tile.Accounts). The choice filters every Sure or Firefly
// dataset in the results before the view sees them, so a tile needs no
// code of its own for it:
//
//	results{"data": Sure (all accounts)} ── scoped ──► Sure (business only) ──► View

import "andon/internal/sources"

// BankAccountsKey is the config key of the accounts a tile reads.
const BankAccountsKey = "bank_accounts"

// Choices of the field; "accounts_all" reads every account.
const (
	bankBoth     = "accounts_all"
	bankBusiness = string(sources.RoleBusiness)
	bankPrivate  = string(sources.RolePrivate)
)

var bankAccountsField = sel(BankAccountsKey, bankBoth, bankBoth, bankBusiness, bankPrivate)

// BankScope is embedded in the config of a tile over bank data.
type BankScope struct {
	Accounts sources.AccountRole // "" = both
}

type bankScoper interface{ setScope(choice string) }

func (b *BankScope) setScope(choice string) {
	b.Accounts = sources.RoleUnknown
	if choice == bankBusiness || choice == bankPrivate {
		b.Accounts = sources.AccountRole(choice)
	}
}

func (b BankScope) scope() sources.AccountRole { return b.Accounts }

// scoped is results with every bank dataset cut to the tile's accounts.
func scoped(cfg any, results map[string]any) map[string]any {
	s, ok := cfg.(interface{ scope() sources.AccountRole })
	if !ok || s.scope() == sources.RoleUnknown {
		return results
	}
	out := make(map[string]any, len(results))
	for k, v := range results {
		if bank, ok := v.(*sources.BankData); ok {
			v = bank.Only(scopeRoles(s.scope())...)
		}
		out[k] = v
	}
	return out
}

// scopeRoles are the accounts a choice reads: business ones with the
// mixed and transfer accounts, private ones with the mixed.
func scopeRoles(r sources.AccountRole) []sources.AccountRole {
	if r == sources.RoleBusiness {
		return sources.BusinessRoles
	}
	return sources.PrivateRoles
}
