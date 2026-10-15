package sources

// Invoice Ninja's recurring expenses: it books them on its own, every
// frequency_id. Andon reads them in months:
//
//	frequency_id  2 weekly · 5 monthly · 6 two months · 7 quarterly
//	              8 four months · 9 half-yearly · 10 yearly · 11 two years
//	              12 three years (1, 3, 4: daily, two and four weeks → 0)

import (
	"context"

	"andon/internal/drivers/services"
)

// ninjaFrequency maps months to Invoice Ninja's frequency_id.
var ninjaFrequency = map[int]int{0: 2, 1: 5, 2: 6, 3: 7, 4: 8, 6: 9, 12: 10, 24: 11, 36: 12}

// ninjaActive is a running recurring entity's status_id.
const ninjaActive = 2

// NinjaFrequency is the frequency_id of a booking every months months;
// ok is false for an interval Invoice Ninja has none for (5 months).
func NinjaFrequency(months int) (id int, ok bool) {
	id, ok = ninjaFrequency[months]
	return id, ok
}

// monthsOf is the interval of frequency_id in months; 0 for shorter ones.
func monthsOf(frequency int) int {
	for months, id := range ninjaFrequency {
		if id == frequency {
			return months
		}
	}
	return 0
}

// loadRecurringExpenses reads the active (not archived) recurring
// expenses; an instance that answers with an error has none for Andon.
func loadRecurringExpenses(ctx context.Context, api services.NinjaApi) []NinjaRecurringExpense {
	raw, err := api.Pages(ctx, "recurring_expenses", nil)
	if err != nil {
		return nil
	}
	var out []NinjaRecurringExpense
	for _, r := range raw {
		m := asMap(r)
		if asBool(m["is_deleted"]) || asFloat(m["archived_at"]) > 0 {
			continue
		}
		out = append(out, NinjaRecurringExpense{
			Key: idKey(m["id"]), Number: asStr(m["number"]), VendorKey: idKey(m["vendor_id"]), CategoryKey: idKey(m["category_id"]),
			Amount: asFloat(m["amount"]), Months: monthsOf(int(asInt64(m["frequency_id"]))), Next: day(m["next_send_date"]),
			Active: asInt64(m["status_id"]) == ninjaActive, Notes: asStr(m["public_notes"]),
		})
	}
	return out
}
