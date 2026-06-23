package execrt

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/dushibing/feishu-plugin-platform/internal/shortcut"
)

// weatherDSL builds the two-step Open-Meteo "city → weather" field shortcut,
// pointed at the test server (full base incl. port). Domains use the bare host
// (no port) — matching the allowlist semantics (urlHost strips the port).
func weatherDSL(base string) shortcut.FieldShortcut {
	host := hostNoPort(base)
	return shortcut.FieldShortcut{
		ID:      "city-weather",
		Title:   shortcut.I18n{ZhCN: "城市天气"},
		Domains: []string{host},
		FormItems: []shortcut.FormItem{
			{Key: "city", Label: shortcut.I18n{ZhCN: "城市"}, Component: "FieldSelect", SupportType: []string{"Text"}, Required: true},
		},
		Steps: []shortcut.Step{
			{ID: "geo", Method: "GET", URL: base + "/geo?name={city}"},
			{ID: "weather", Method: "GET", URL: base + "/forecast?lat={geo.results.0.latitude}&lon={geo.results.0.longitude}"},
		},
		Result: shortcut.Result{
			Kind: "object",
			Properties: []shortcut.ResultProp{
				{Key: "temperature", Type: "Number", Label: shortcut.I18n{ZhCN: "温度"}, Primary: true, Expr: "res.current.temperature_2m"},
				{Key: "wind_speed", Type: "Number", Label: shortcut.I18n{ZhCN: "风速"}, Expr: "res.current.wind_speed_10m"},
			},
		},
	}
}

func mockWeatherServer() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/geo", func(w http.ResponseWriter, r *http.Request) {
		// echo that the input flowed through, then return coords
		if r.URL.Query().Get("name") == "" {
			http.Error(w, "missing name", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []any{map[string]any{"latitude": 39.9075, "longitude": 116.4}},
		})
	})
	mux.HandleFunc("/forecast", func(w http.ResponseWriter, r *http.Request) {
		// prove the prior step's coords were chained into this request
		if r.URL.Query().Get("lat") != "39.9075" {
			http.Error(w, "lat not chained: "+r.URL.RawQuery, http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"current": map[string]any{"temperature_2m": 28.8, "wind_speed_10m": 13.4},
		})
	})
	return httptest.NewServer(mux)
}

// testEngine allows private (loopback) addresses since httptest binds 127.0.0.1.
func testEngine() *Engine { return New(Options{AllowPrivate: true}) }

func hostNoPort(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return u.Hostname() // strips the port → matches domain allowlist semantics
}

func TestRunMultiStepWeather(t *testing.T) {
	srv := mockWeatherServer()
	defer srv.Close()

	out, err := testEngine().Run(context.Background(), weatherDSL(srv.URL), map[string]any{"city": "Beijing"}, nil)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	temp, _ := toNum(out["temperature"])
	wind, _ := toNum(out["wind_speed"])
	if math.Abs(temp-28.8) > 1e-9 {
		t.Errorf("temperature = %v, want 28.8", out["temperature"])
	}
	if math.Abs(wind-13.4) > 1e-9 {
		t.Errorf("wind_speed = %v, want 13.4", out["wind_speed"])
	}
}

func TestRunRejectsDomainOutsideAllowlist(t *testing.T) {
	srv := mockWeatherServer()
	defer srv.Close()

	dsl := weatherDSL(srv.URL)
	// Point the second step at a host NOT in the allowlist → must be refused
	// before any request goes out.
	dsl.Steps[1].URL = "http://evil.example.com/forecast?lat={geo.results.0.latitude}"
	_, err := testEngine().Run(context.Background(), dsl, map[string]any{"city": "Beijing"}, nil)
	if err == nil || !strings.Contains(err.Error(), "evil.example.com") {
		t.Fatalf("expected domain-allowlist rejection, got %v", err)
	}
}

func TestSSRFGuardBlocksPrivateAddress(t *testing.T) {
	// A public-looking allowlisted host is irrelevant here: the guard inspects the
	// dialed IP. Point at loopback with the guard ON (default engine).
	srv := mockWeatherServer()
	defer srv.Close()

	dsl := shortcut.FieldShortcut{
		ID:        "ssrf",
		Title:     shortcut.I18n{ZhCN: "x"},
		Domains:   []string{hostNoPort(srv.URL)},
		FormItems: []shortcut.FormItem{{Key: "city", Label: shortcut.I18n{ZhCN: "城市"}, Component: "FieldSelect", SupportType: []string{"Text"}, Required: true}},
		Execute:   shortcut.Execute{URL: srv.URL + "/geo?name={city}", Method: "GET"},
		Result: shortcut.Result{Kind: "object", Properties: []shortcut.ResultProp{
			{Key: "lat", Type: "Number", Primary: true, Expr: "res.results.0.latitude"},
		}},
	}
	guarded := New(Options{}) // AllowPrivate=false
	_, err := guarded.Run(context.Background(), dsl, map[string]any{"city": "Beijing"}, nil)
	if err == nil || !strings.Contains(err.Error(), "private address") {
		t.Fatalf("expected SSRF guard to block loopback, got %v", err)
	}
}

func TestRunSingleWithQueryParamAuth(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.URL.Query().Get("appid")
		_ = json.NewEncoder(w).Encode(map[string]any{"main": map[string]any{"temp": 21.0}})
	}))
	defer srv.Close()

	dsl := shortcut.FieldShortcut{
		ID:      "owm",
		Title:   shortcut.I18n{ZhCN: "天气"},
		Domains: []string{hostNoPort(srv.URL)},
		Auth: &shortcut.Auth{
			ID: "weatherApiKey", Type: "QueryParamToken", Label: "OWM Key",
			Platform: "OpenWeatherMap", InstructionsURL: "https://openweathermap.org/appid", ParamName: "appid",
		},
		FormItems: []shortcut.FormItem{{Key: "city", Label: shortcut.I18n{ZhCN: "城市"}, Component: "FieldSelect", SupportType: []string{"Text"}, Required: true}},
		Execute:   shortcut.Execute{URL: srv.URL + "/weather?q={city}", Method: "GET"},
		Result: shortcut.Result{Kind: "object", Properties: []shortcut.ResultProp{
			{Key: "temperature", Type: "Number", Primary: true, Expr: "res.main.temp"},
		}},
	}
	out, err := New(Options{AllowPrivate: true}).Run(context.Background(), dsl, map[string]any{"city": "Beijing"}, map[string]string{"weatherApiKey": "secret123"})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if gotKey != "secret123" {
		t.Errorf("server saw appid=%q, want secret123 (auth not injected)", gotKey)
	}
	if temp, _ := toNum(out["temperature"]); math.Abs(temp-21.0) > 1e-9 {
		t.Errorf("temperature = %v, want 21", out["temperature"])
	}
}

func TestRunRejectsInvalidDSL(t *testing.T) {
	// Missing title + no form items → Validate() fails before any request.
	_, err := testEngine().Run(context.Background(), shortcut.FieldShortcut{ID: "bad"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid shortcut") {
		t.Fatalf("expected validation failure, got %v", err)
	}
}
