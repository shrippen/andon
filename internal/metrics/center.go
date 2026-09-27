package metrics

import "sort"

// Center is how a typical value is taken from a list: the mean, or the
// median, which one outlier cannot drag (one invoice paid after 90 days
// among many paid after 10).
type Center string

const (
	CenterMean   Center = "mean"
	CenterMedian Center = "median"
)

// CenterOf reads the space's choice (settings.stats.center); mean when unset.
func CenterOf(settings map[string]any) Center {
	stats, _ := settings["stats"].(map[string]any)
	if c, _ := stats["center"].(string); Center(c) == CenterMedian {
		return CenterMedian
	}
	return CenterMean
}

// Of returns the typical value of values; 0 for none.
func (c Center) Of(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	if c == CenterMedian {
		sorted := append([]float64(nil), values...)
		sort.Float64s(sorted)
		mid := len(sorted) / 2
		if len(sorted)%2 == 1 {
			return sorted[mid]
		}
		return (sorted[mid-1] + sorted[mid]) / 2
	}
	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// days is Of for whole days, rounded.
func (c Center) days(values []int) int {
	list := make([]float64, len(values))
	for i, v := range values {
		list[i] = float64(v)
	}
	return int(round(c.Of(list)))
}
