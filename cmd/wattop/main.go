// Command wattop is the terminal dashboard: SoC watts and AI coding-agent
// dollars, read on one refresh clock and joined by pid. See docs/plans for
// the full design; this file only wires the real collectors, resolves
// flags/env/config, and dispatches to the TUI, --json/--once headless
// output, or the doctor subcommand.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jasonm4130/wattop/internal/agent/claude"
	"github.com/jasonm4130/wattop/internal/agent/codex"
	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/pricing"
	"github.com/jasonm4130/wattop/internal/proc"
	"github.com/jasonm4130/wattop/internal/soc"
	"github.com/jasonm4130/wattop/internal/state"
	"github.com/jasonm4130/wattop/internal/ui"
	"github.com/jasonm4130/wattop/internal/ui/theme"
	"github.com/jasonm4130/wattop/internal/version"
)

// defaultInterval is the SoC block duration when neither --interval nor
// config.toml's interval_ms sets one.
const defaultInterval = time.Second

// minInterval and maxInterval bound --interval / config's interval_ms: it
// is the SoC block duration and therefore the whole cycle period, so a
// value outside this range is clamped (with a warning) rather than
// accepted, per the Task 13 spec.
const (
	minInterval = 500 * time.Millisecond
	maxInterval = 5 * time.Second
)

// burnWindow and burnAlpha are the BurnTracker's sliding window and EWMA
// smoothing factor, per the plan's burn.go spec.
const (
	burnWindow = 60 * time.Second
	burnAlpha  = 0.3
)

// cliFlags is the parsed top-level flag surface: --theme, --interval,
// --json, --once, --no-color, --version. Pulled out of main() so
// run_test.go can assert the flag surface parses without exec'ing the
// built binary.
type cliFlags struct {
	theme    string
	interval time.Duration
	json     bool
	once     bool
	noColor  bool
	version  bool
}

func parseFlags(args []string) (cliFlags, error) {
	var f cliFlags
	fs := flag.NewFlagSet("wattop", flag.ContinueOnError)
	fs.StringVar(&f.theme, "theme", "", "theme name or bare hex accent (default: $WATTOP_THEME, then config.toml, then wattop-dark)")
	fs.DurationVar(&f.interval, "interval", 0, "SoC sample interval, 500ms-5s (default: config.toml, then 1s)")
	fs.BoolVar(&f.json, "json", false, "print one Snapshot per interval as NDJSON and never enter the alt screen")
	fs.BoolVar(&f.once, "once", false, "with --json, print exactly one Snapshot and exit")
	fs.BoolVar(&f.noColor, "no-color", false, "disable all ANSI styling")
	fs.BoolVar(&f.version, "version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		return cliFlags{}, err
	}
	return f, nil
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "doctor" {
		runDoctorCommand(os.Args[2:])
		return
	}

	flags, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "wattop: %v\n", err)
		os.Exit(2)
	}

	if flags.version {
		fmt.Println(version.String())
		return
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "wattop: %v\n", err)
	}

	interval := resolveInterval(flags.interval, cfg)
	themeName := resolveThemeName(flags.theme, cfg)
	roles, resolvedThemeName := loadThemeOrWarn(themeName)
	noColor := flags.noColor || os.Getenv("NO_COLOR") != ""

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	src, book, sysAvailable := buildSources(ctx, cfg)
	burn := pricing.NewBurnTracker(burnWindow, burnAlpha)
	st := state.New(book, burn)
	loop := NewLoop(src, interval, st, sysAvailable)

	if flags.json {
		if flags.once {
			if _, err := runOnce(ctx, loop, os.Stdout); err != nil {
				fmt.Fprintf(os.Stderr, "wattop: %v\n", err)
				os.Exit(1)
			}
			return
		}
		if err := runJSON(ctx, loop, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "wattop: %v\n", err)
			os.Exit(1)
		}
		return
	}

	model := ui.New(st, resolvedThemeName, roles).WithNoColor(noColor)
	if err := runInteractive(ctx, loop, model); err != nil {
		fmt.Fprintf(os.Stderr, "wattop: %v\n", err)
		os.Exit(1)
	}
}

// runDoctorCommand parses doctor's own tiny flag set (--ioreport-groups)
// and runs the report, without touching the main flag set above.
func runDoctorCommand(args []string) {
	fs := flag.NewFlagSet("wattop doctor", flag.ExitOnError)
	groups := fs.Bool("ioreport-groups", false, "enumerate IOReport groups with channel counts")
	_ = fs.Parse(args)

	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "wattop: %v\n", err)
	}
	interval := resolveInterval(0, cfg)
	themeName := resolveThemeName("", cfg)
	_, resolvedThemeName := loadThemeOrWarn(themeName)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	src, book, _ := buildSources(ctx, cfg)
	runDoctor(ctx, os.Stdout, src, interval, book, resolvedThemeName, *groups)
}

// buildSources constructs the real, non-replay Sources: the CGO soc
// sampler (or its Linux/non-Apple-Silicon stub), the process scanner, and
// the Claude and Codex agent sources. It also constructs and kicks off a
// background refresh of the one pricing.Book the whole process shares.
//
// soc.Init() is called once here (never inside Loop's own retry path on
// this first attempt) so a startup failure gets main's one-line banner:
// "run with the session half only" per the spec, rather than a silent
// disabled sampler.
func buildSources(ctx context.Context, cfg Config) (Sources, *pricing.Book, bool) {
	sampler := soc.NewSampler()
	sysAvailable := true
	if err := sampler.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "wattop: soc unavailable (%v); running with the session half only\n", err)
		sysAvailable = false
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "wattop: could not resolve home directory: %v\n", err)
	}

	claudeSessions := filepath.Join(home, ".claude", "sessions")
	claudeProjects := filepath.Join(home, ".claude", "projects")
	codexRoot := filepath.Join(home, ".codex", "sessions")

	idleThreshold := codex.DefaultIdleThreshold
	if cfg.CodexStaleMinutes > 0 {
		idleThreshold = time.Duration(cfg.CodexStaleMinutes) * time.Minute
	}

	book, err := pricing.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "wattop: loading pricing table: %v\n", err)
		os.Exit(1)
	}
	book.Refresh(ctx)

	src := Sources{
		Sys:  sampler,
		Proc: proc.NewScanner(),
		Agents: []domain.AgentSource{
			claude.NewSource(claudeSessions, claudeProjects, cfg.ContextWindowOverrides),
			codex.NewSource(codexRoot, idleThreshold),
		},
		// The same book state.New holds, kept here as well so each cycle
		// can report the pricing source's health -- Refresh's failures are
		// invisible otherwise (see pricingHealth), and main keeps its own
		// reference for doctor regardless.
		Pricing: book,
	}
	return src, book, sysAvailable
}

// resolveInterval applies the --interval / config.toml precedence and
// clamps the result to [minInterval, maxInterval] with a warning, never
// fatally.
func resolveInterval(flagValue time.Duration, cfg Config) time.Duration {
	interval := flagValue
	if interval == 0 && cfg.IntervalMs > 0 {
		interval = time.Duration(cfg.IntervalMs) * time.Millisecond
	}
	if interval == 0 {
		interval = defaultInterval
	}
	if interval < minInterval {
		fmt.Fprintf(os.Stderr, "wattop: --interval %s is below the %s floor; clamping\n", interval, minInterval)
		interval = minInterval
	}
	if interval > maxInterval {
		fmt.Fprintf(os.Stderr, "wattop: --interval %s is above the %s ceiling; clamping\n", interval, maxInterval)
		interval = maxInterval
	}
	return interval
}

// resolveThemeName applies the --theme > $WATTOP_THEME > config.toml >
// wattop-dark precedence stated in the Task 13 spec.
func resolveThemeName(flagValue string, cfg Config) string {
	if flagValue != "" {
		return flagValue
	}
	if v := os.Getenv("WATTOP_THEME"); v != "" {
		return v
	}
	if cfg.Theme != "" {
		return cfg.Theme
	}
	return "wattop-dark"
}

// loadThemeOrWarn resolves name via theme.Load, falling back to
// "wattop-dark" with a one-line stderr warning on an unrecognised name --
// an unknown theme is never fatal. It returns the resolved Roles and the
// name actually used (for ui.New's initial theme-cycle position and for
// doctor's report).
func loadThemeOrWarn(name string) (theme.Roles, string) {
	roles, err := theme.Load(name)
	if err == nil {
		return roles, name
	}
	fmt.Fprintf(os.Stderr, "wattop: %v; falling back to wattop-dark\n", err)
	roles, err = theme.Load("wattop-dark")
	if err != nil {
		// The embedded default palette failing to load is a build defect,
		// not a runtime condition callers can recover from.
		panic(fmt.Sprintf("wattop: embedded default theme wattop-dark failed to load: %v", err))
	}
	return roles, "wattop-dark"
}
