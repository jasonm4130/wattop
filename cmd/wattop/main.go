package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jasonm4130/wattop/internal/agent/claude"
	"github.com/jasonm4130/wattop/internal/agent/codex"
	"github.com/jasonm4130/wattop/internal/collect/demo"
	"github.com/jasonm4130/wattop/internal/domain"
	"github.com/jasonm4130/wattop/internal/pricing"
	"github.com/jasonm4130/wattop/internal/proc"
	"github.com/jasonm4130/wattop/internal/soc"
	"github.com/jasonm4130/wattop/internal/state"
	"github.com/jasonm4130/wattop/internal/ui"
	"github.com/jasonm4130/wattop/internal/ui/theme"
	"github.com/jasonm4130/wattop/internal/version"
)

const defaultInterval = time.Second

const (
	minInterval = 500 * time.Millisecond
	maxInterval = 5 * time.Second
)

type cliFlags struct {
	theme    string
	interval time.Duration
	json     bool
	once     bool
	noColor  bool
	version  bool
	demo     bool
}

func newFlagSet(f *cliFlags) *flag.FlagSet {
	fs := flag.NewFlagSet("wattop", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	fs.StringVar(&f.theme, "theme", "", "theme name or bare hex accent (default: $WATTOP_THEME, then config.toml, then wattop-dark)")
	fs.DurationVar(&f.interval, "interval", 0, "SoC sample interval, 500ms-5s (default: config.toml, then 1s)")
	fs.BoolVar(&f.json, "json", false, "print one Snapshot per interval as NDJSON and never enter the alt screen")
	fs.BoolVar(&f.once, "once", false, "with --json, print exactly one Snapshot and exit")
	fs.BoolVar(&f.noColor, "no-color", false, "disable all ANSI styling")
	fs.BoolVar(&f.version, "version", false, "print the version and exit")
	fs.BoolVar(&f.demo, "demo", false, "synthetic data for screenshots")
	return fs
}

func newDoctorFlagSet(groups *bool) *flag.FlagSet {
	fs := flag.NewFlagSet("wattop doctor", flag.ExitOnError)
	fs.BoolVar(groups, "ioreport-groups", false, "enumerate IOReport groups with channel counts")
	return fs
}

func parseFlags(args []string) (cliFlags, error) {
	var f cliFlags
	if err := newFlagSet(&f).Parse(args); err != nil {
		return cliFlags{}, err
	}
	return f, nil
}

func writeUsage(w io.Writer) {
	var f cliFlags
	fs := newFlagSet(&f)
	fs.SetOutput(w)
	fmt.Fprintf(w, "Usage:\n  wattop [flags]\n  wattop doctor [doctor flags]\n\nFlags:\n")
	fs.PrintDefaults()

	var groups bool
	dfs := newDoctorFlagSet(&groups)
	dfs.SetOutput(w)
	fmt.Fprintf(w, "\nCommands:\n  doctor\tprint which SoC channels, agent sources, pricing and theme resolve on this machine\n\nDoctor flags:\n")
	dfs.PrintDefaults()
}

func parseErrorExit(err error, stdout, stderr io.Writer) int {
	if errors.Is(err, flag.ErrHelp) {
		writeUsage(stdout)
		return 0
	}
	fmt.Fprintf(stderr, "wattop: %v\n", err)
	writeUsage(stderr)
	return 2
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "doctor" {
		runDoctorCommand(os.Args[2:])
		return
	}

	flags, err := parseFlags(os.Args[1:])
	if err != nil {
		os.Exit(parseErrorExit(err, os.Stdout, os.Stderr))
	}

	if flags.version {
		fmt.Println(version.String())
		return
	}

	var cfg Config
	if !flags.demo {
		cfg, err = loadConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "wattop: %v\n", err)
		}
	}

	interval := resolveInterval(flags.interval, cfg)
	themeName := resolveThemeName(flags.theme, cfg)
	roles, resolvedThemeName := loadThemeOrWarn(themeName)
	noColor := flags.noColor || os.Getenv("NO_COLOR") != ""

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var (
		src          Sources
		book         *pricing.Book
		sysAvailable bool
		world        *demo.World
	)
	if flags.demo {
		src, book, world, err = buildDemoSources(interval)
		if err != nil {
			fmt.Fprintf(os.Stderr, "wattop: %v\n", err)
			os.Exit(1)
		}
		sysAvailable = true
	} else {
		src, book, sysAvailable = buildSources(ctx, cfg, pricingLogger(!flags.json))
	}
	burn := pricing.NewBurnTracker(pricing.DefaultBurnWindow, pricing.DefaultBurnAlpha)
	st := state.New(book, burn)
	if world != nil {
		prewarmDemo(st, world, time.Now(), demoPrewarm)
	}
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

	model := ui.New(st, resolvedThemeName, roles).WithNoColor(noColor).WithBurnHot(cfg.BurnHotUSDPerHr)
	if err := runInteractive(ctx, loop, model); err != nil {
		fmt.Fprintf(os.Stderr, "wattop: %v\n", err)
		os.Exit(1)
	}
}

func runDoctorCommand(args []string) {
	groups := new(bool)
	fs := newDoctorFlagSet(groups)
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

	src, book, _ := buildSources(ctx, cfg, pricingLogger(false))
	runDoctor(ctx, os.Stdout, src, interval, book, resolvedThemeName, *groups)
}

func pricingLogger(tuiOwnsTerminal bool) *log.Logger {
	if tuiOwnsTerminal {
		return log.New(io.Discard, "", 0)
	}
	return log.New(os.Stderr, "", log.LstdFlags)
}

func buildSources(ctx context.Context, cfg Config, pricingLog *log.Logger) (Sources, *pricing.Book, bool) {
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
	book.Refresh(ctx, pricingLog)

	src := Sources{
		Sys:  sampler,
		Proc: proc.NewScanner(),
		Agents: []domain.AgentSource{
			claude.NewSource(claudeSessions, claudeProjects, cfg.ContextWindowOverrides),
			codex.NewSource(codexRoot, idleThreshold),
		},
		Pricing: book,
	}
	return src, book, sysAvailable
}

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

func loadThemeOrWarn(name string) (theme.Roles, string) {
	roles, err := theme.Load(name)
	if err == nil {
		return roles, name
	}
	fmt.Fprintf(os.Stderr, "wattop: %v; falling back to wattop-dark\n", err)
	roles, err = theme.Load("wattop-dark")
	if err != nil {
		panic(fmt.Sprintf("wattop: embedded default theme wattop-dark failed to load: %v", err))
	}
	return roles, "wattop-dark"
}
