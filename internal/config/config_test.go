package config

import (
	"errors"
	"strings"
	"testing"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoad_ReturnsAuto_WhenNothingSet(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil || cfg.Theme != ThemeAuto {
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

func TestLoad_KnowsEveryThemeName(t *testing.T) {
	for _, name := range []string{ThemeDark, ThemeLight, ThemeMono, ThemeANSI, ThemeContrast, ThemeAuto} {
		cfg, err := Load(env(map[string]string{EnvTheme: name}))
		if err != nil || cfg.Theme != name {
			t.Errorf("%s: %+v %v", name, cfg, err)
		}
	}
	_, err := Load(env(map[string]string{EnvTheme: "neon"}))
	if err == nil || !errors.Is(err, ErrInvalidTheme) || !strings.Contains(err.Error(), "auto") || !strings.Contains(err.Error(), "ansi") || !strings.Contains(err.Error(), "contrast") {
		t.Errorf("the error names the themes: %v", err)
	}
}

func TestThemeNames_ListsEveryTheme(t *testing.T) {
	names := ThemeNames()
	if len(names) != 6 || names[0] != ThemeAuto {
		t.Fatalf("%v", names)
	}
	for _, name := range names {
		if _, err := Load(env(map[string]string{EnvTheme: name})); err != nil {
			t.Errorf("%s is listed but not accepted: %v", name, err)
		}
	}
}
