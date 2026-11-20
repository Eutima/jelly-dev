package config

import (
	"errors"
	"testing"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoad_ReturnsDark_WhenNothingSet(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil || cfg.Theme != ThemeDark {
		t.Fatalf("%+v %v", cfg, err)
	}
}

func TestLoad_ReturnsNamedTheme_WhenThemeSetIgnoringCaseAndSpaces(t *testing.T) {
	cfg, err := Load(env(map[string]string{EnvTheme: " Light "}))
	if err != nil || cfg.Theme != ThemeLight {
		t.Fatalf("%+v %v", cfg, err)
	}
}

func TestLoad_ReturnsMono_WhenNoColorSet(t *testing.T) {
	cfg, err := Load(env(map[string]string{EnvNoColor: "1"}))
	if err != nil || cfg.Theme != ThemeMono {
		t.Fatalf("%+v %v", cfg, err)
	}
}

func TestLoad_PrefersExplicitTheme_WhenNoColorAlsoSet(t *testing.T) {
	cfg, err := Load(env(map[string]string{EnvNoColor: "1", EnvTheme: "dark"}))
	if err != nil || cfg.Theme != ThemeDark {
		t.Fatalf("%+v %v", cfg, err)
	}
}

func TestLoad_ReturnsError_WhenThemeUnknown(t *testing.T) {
	if _, err := Load(env(map[string]string{EnvTheme: "neon"})); !errors.Is(err, ErrInvalidTheme) {
		t.Fatalf("err %v", err)
	}
}
