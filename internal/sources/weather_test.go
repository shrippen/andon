package sources_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"andon/internal/sources"
)

// TestWeatherHours: the next hours come with temperature and rain chance.
func TestWeatherHours(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("forecast_hours") != "24" {
			t.Errorf("forecast_hours: %s", r.URL.RawQuery)
		}
		w.Write([]byte(`{"current":{"temperature_2m":14,"weather_code":3,"is_day":1},
			"daily":{"time":["2026-09-27"],"weather_code":[3],"temperature_2m_max":[16],"temperature_2m_min":[9]},
			"hourly":{"time":["2026-09-27T14:00","2026-09-27T15:00"],"temperature_2m":[14.2,13.8],"precipitation_probability":[10,70]}}`))
	}))
	defer srv.Close()
	defer sources.SetWeatherURL(srv.URL)()

	out, err := sources.WeatherSource{}.Fetch(context.Background(), sources.Ctx{Params: map[string]any{"lat": 52.5, "lon": 13.4}})
	if err != nil {
		t.Fatal(err)
	}
	w := out.(*sources.WeatherResult)
	if len(w.Hours) != 2 || w.Hours[1].Rain != 70 || w.Hours[0].Temp != 14.2 {
		t.Fatalf("hours: %+v", w.Hours)
	}
}

// TestWeatherDays: a tile asking for a week gets today plus seven days.
func TestWeatherDays(t *testing.T) {
	got := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("forecast_days")
		w.Write([]byte(`{"current":{},"daily":{},"hourly":{}}`))
	}))
	defer srv.Close()
	defer sources.SetWeatherURL(srv.URL)()

	week := sources.Ctx{Params: map[string]any{"days": 7.0}}
	if _, err := (sources.WeatherSource{}).Fetch(context.Background(), week); err != nil || got != "8" {
		t.Fatalf("forecast_days %q, %v", got, err)
	}
	(sources.WeatherSource{}).Fetch(context.Background(), sources.Ctx{Params: map[string]any{}})
	if got != "4" {
		t.Fatalf("default forecast_days %q", got)
	}
}
