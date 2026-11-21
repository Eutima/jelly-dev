// Package config reads the tool configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"strings"
)

// Environment variable names.
const (
	EnvTheme   = "JELLY_DEV_THEME"
	EnvNoColor = "NO_COLOR" // https://no-color.org: any non-empty value disables colors
)

// Theme names.
const (
	ThemeDark     = "dark"
	ThemeLight    = "light"
	ThemeMono     = "mono"
	ThemeANSI     = "ansi"     // for terminals with 16 colours: reverse video and the terminal's own colours, no fixed backgrounds
	ThemeContrast = "contrast" // high contrast: bold, reverse video, no dim text
	ThemeAuto     = "auto"     // the default: chosen from the number of colours of the terminal and its background
)

// ThemeNames lists every theme name, `auto` first.
func ThemeNames() []string {
	return []string{ThemeAuto, ThemeDark, ThemeLight, ThemeANSI, ThemeContrast, ThemeMono}
}

// ErrInvalidTheme is returned for an unknown JELLY_DEV_THEME value.
var ErrInvalidTheme = errors.New("invalid theme")

// Config is the tool configuration.
type Config struct {
	Theme string
}

// Load builds the configuration from environment variables read through getenv.
// An explicit JELLY_DEV_THEME wins; otherwise NO_COLOR selects the mono theme and auto (the theme that suits the terminal) is the default.
func Load(getenv func(string) string) (Config, error) {
	raw := getenv(EnvTheme)
	name := strings.ToLower(strings.TrimSpace(raw))
	switch name {
	case ThemeDark, ThemeLight, ThemeMono, ThemeANSI, ThemeContrast, ThemeAuto:
		return Config{Theme: name}, nil
	case "":
		if getenv(EnvNoColor) != "" {
			return Config{Theme: ThemeMono}, nil
		}
		return Config{Theme: ThemeAuto}, nil
	}
	return Config{}, fmt.Errorf("%w: %s=%q (use %s)", ErrInvalidTheme, EnvTheme, raw, strings.Join(ThemeNames(), ", "))
}
