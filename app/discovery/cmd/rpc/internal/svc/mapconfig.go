package svc

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// MapSettings is startup configuration owned by Discovery. It is never
// returned verbatim: MapConfig builds the public projection from it.
type MapSettings struct {
	Provider         string
	ServiceProvider  string
	DefaultCity      string
	DefaultAdcode    string
	CenterLatitude   float64
	CenterLongitude  float64
	AmapJSKey        string
	AmapSecurityCode string
	AmapWebKey       string
	TileURL          string
	Attribution      string
}

type mapFileSettings struct {
	Provider         string   `json:"provider"`
	ServiceProvider  string   `json:"map_service_provider"`
	DefaultCity      string   `json:"default_city"`
	DefaultAdcode    string   `json:"default_adcode"`
	DefaultLatitude  *float64 `json:"default_latitude"`
	DefaultLongitude *float64 `json:"default_longitude"`
	JSKey            string   `json:"amap_js_key"`
	SecurityCode     string   `json:"amap_security_code"`
	WebKey           string   `json:"amap_web_service_key"`
	TileURL          *string  `json:"tile_url"`
	Attribution      *string  `json:"attribution"`
}

func loadMapSettings(path string) (MapSettings, error) {
	settings := MapSettings{
		Provider:        "leaflet",
		ServiceProvider: "amap",
		DefaultCity:     "桂林市",
		DefaultAdcode:   "450300",
		CenterLatitude:  25.274,
		CenterLongitude: 110.290,
		TileURL:         "https://tile.openstreetmap.org/{z}/{x}/{y}.png",
		Attribution:     "© OpenStreetMap contributors",
	}
	if path == "" {
		path = "configs/map.local.json"
	}
	body, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return MapSettings{}, fmt.Errorf("read map config: %w", err)
	}
	if err == nil {
		var file mapFileSettings
		if err := json.Unmarshal(body, &file); err != nil {
			return MapSettings{}, fmt.Errorf("invalid map config JSON: %w", err)
		}
		if strings.TrimSpace(file.Provider) != "" {
			settings.Provider = strings.TrimSpace(file.Provider)
		}
		if strings.TrimSpace(file.ServiceProvider) != "" {
			settings.ServiceProvider = strings.TrimSpace(file.ServiceProvider)
		}
		if strings.TrimSpace(file.DefaultCity) != "" {
			settings.DefaultCity = strings.TrimSpace(file.DefaultCity)
		}
		if strings.TrimSpace(file.DefaultAdcode) != "" {
			settings.DefaultAdcode = strings.TrimSpace(file.DefaultAdcode)
		}
		if file.DefaultLatitude != nil {
			settings.CenterLatitude = *file.DefaultLatitude
		}
		if file.DefaultLongitude != nil {
			settings.CenterLongitude = *file.DefaultLongitude
		}
		settings.AmapJSKey = strings.TrimSpace(file.JSKey)
		settings.AmapSecurityCode = strings.TrimSpace(file.SecurityCode)
		settings.AmapWebKey = strings.TrimSpace(file.WebKey)
		if file.TileURL != nil {
			settings.TileURL = *file.TileURL
		}
		if file.Attribution != nil {
			settings.Attribution = *file.Attribution
		}
	}
	overrideString(&settings.Provider, "MAP_PROVIDER")
	overrideString(&settings.ServiceProvider, "MAP_SERVICE_PROVIDER")
	overrideString(&settings.DefaultCity, "MAP_DEFAULT_CITY")
	overrideString(&settings.DefaultAdcode, "MAP_DEFAULT_ADCODE")
	overrideString(&settings.AmapJSKey, "AMAP_JS_KEY")
	overrideString(&settings.AmapSecurityCode, "AMAP_SECURITY_CODE")
	overrideString(&settings.AmapWebKey, "AMAP_WEB_SERVICE_KEY")
	overrideString(&settings.TileURL, "MAP_TILE_URL")
	overrideString(&settings.Attribution, "MAP_ATTRIBUTION")
	if value, ok := os.LookupEnv("MAP_DEFAULT_LATITUDE"); ok {
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return MapSettings{}, fmt.Errorf("MAP_DEFAULT_LATITUDE must be a coordinate")
		}
		settings.CenterLatitude = parsed
	}
	if value, ok := os.LookupEnv("MAP_DEFAULT_LONGITUDE"); ok {
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return MapSettings{}, fmt.Errorf("MAP_DEFAULT_LONGITUDE must be a coordinate")
		}
		settings.CenterLongitude = parsed
	}
	if settings.Provider == "" {
		settings.Provider = "leaflet"
	}
	if settings.ServiceProvider == "" {
		settings.ServiceProvider = "amap"
	}
	if settings.Provider != "leaflet" && settings.Provider != "amap" {
		return MapSettings{}, fmt.Errorf("MAP_PROVIDER must be leaflet or amap")
	}
	if settings.Provider == "amap" && settings.AmapJSKey == "" {
		return MapSettings{}, fmt.Errorf("AMAP_JS_KEY is required for amap")
	}
	if settings.ServiceProvider != "amap" && settings.ServiceProvider != "local" && settings.ServiceProvider != "hybrid" {
		return MapSettings{}, fmt.Errorf("MAP_SERVICE_PROVIDER must be local, amap or hybrid")
	}
	return settings, nil
}

func overrideString(target *string, name string) {
	if value, ok := os.LookupEnv(name); ok {
		*target = strings.TrimSpace(value)
	}
}
