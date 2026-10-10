package metrics

// LLM spend across providers (sources.LLMDataset):
//
//	LLMAccounts   every account of the datasets, in provider order
//	LLMTotalsOf   today, month, tokens and requests over accounts
//	LLMDaily      an account's cost per day, oldest first, ending today
//	LLMModels     per model over the month, most expensive first
//	LLMSpike      today's cost against the days before
//	LLMPace       month spend projected to the month's end

import (
	"slices"
	"sort"
	"time"

	"andon/internal/sources"
)

// LLMAccounts lists the accounts of every LLM dataset, by provider and name.
func LLMAccounts(datasets map[string]any) []sources.LLMAccount {
	var out []sources.LLMAccount
	for _, s := range sources.LLMServices() {
		if d, ok := datasets[string(s)].(*sources.LLMDataset); ok && d != nil {
			out = append(out, d.Accounts...)
		}
	}
	return out
}

// LLMTotals is spend and use over accounts.
type LLMTotals struct {
	Today, Month float64 // USD
	Tokens       int64   // this month, input + cached + output
	Requests     int64
}

// LLMTotalsOf sums the accounts that report spend.
func LLMTotalsOf(accounts []sources.LLMAccount, today time.Time) LLMTotals {
	var t LLMTotals
	month := today.UTC().Format("2006-01")
	for _, a := range accounts {
		t.Today += a.Today
		t.Month += a.Month
		for _, u := range a.Uses {
			if u.Day[:len(month)] == month {
				t.Tokens += u.Input + u.Cached + u.Output
				t.Requests += u.Requests
			}
		}
	}
	return t
}

// LLMDaily is an account's cost per day for n days ending today.
func LLMDaily(a sources.LLMAccount, today time.Time, n int) []float64 {
	out := make([]float64, n)
	first := today.UTC().AddDate(0, 0, 1-n)
	index := map[string]int{}
	for i := range n {
		index[first.AddDate(0, 0, i).Format(time.DateOnly)] = i
	}
	for _, u := range a.Uses {
		if i, ok := index[u.Day]; ok {
			out[i] += u.Cost
		}
	}
	return out
}

// LLMModelRow is one model's month.
type LLMModelRow struct {
	Service                         string
	Model                           string
	Input, Cached, Output, Requests int64
	Cost                            float64
}

// LLMModels sums each account's models over the month, most expensive
// first, then by tokens.
func LLMModels(accounts []sources.LLMAccount, today time.Time) []LLMModelRow {
	month := today.UTC().Format("2006-01")
	var out []LLMModelRow
	index := map[[2]string]int{}
	for _, a := range accounts {
		for _, u := range a.Uses {
			if u.Day[:len(month)] != month {
				continue
			}
			k := [2]string{string(a.Service), u.Model}
			i, ok := index[k]
			if !ok {
				i = len(out)
				index[k] = i
				out = append(out, LLMModelRow{Service: k[0], Model: u.Model})
			}
			r := &out[i]
			r.Input += u.Input
			r.Cached += u.Cached
			r.Output += u.Output
			r.Requests += u.Requests
			r.Cost += u.Cost
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Cost != out[j].Cost {
			return out[i].Cost > out[j].Cost
		}
		return out[i].Input+out[i].Output > out[j].Input+out[j].Output
	})
	return out
}

// LLMSpike is today's cost and the mean of the n days before; a day
// without use counts as 0. ok is false without history.
func LLMSpike(a sources.LLMAccount, today time.Time, n int) (now, mean float64, ok bool) {
	if len(a.Uses) == 0 {
		return 0, 0, false
	}
	days := LLMDaily(a, today, n+1)
	return days[n], sum(days[:n]) / float64(n), true
}

// LLMPace projects the month's spend so far to its last day:
// 20 $ on the 10th of a 30-day month → 60 $.
func LLMPace(month float64, today time.Time) float64 {
	t := today.UTC()
	daysIn := time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	return month / float64(t.Day()) * float64(daysIn)
}

func sum(values []float64) float64 {
	total := 0.0
	for _, v := range values {
		total += v
	}
	return total
}

// LLMQuotaTop is the fullest plan window of an account, false without one.
func LLMQuotaTop(a sources.LLMAccount) (sources.LLMQuota, bool) {
	if len(a.Quotas) == 0 {
		return sources.LLMQuota{}, false
	}
	return slices.MaxFunc(a.Quotas, func(x, y sources.LLMQuota) int {
		switch {
		case x.Percent < y.Percent:
			return -1
		case x.Percent > y.Percent:
			return 1
		}
		return 0
	}), true
}
