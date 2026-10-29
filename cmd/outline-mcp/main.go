package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"outline-mcp/internal/app"
	"outline-mcp/internal/config"
	"outline-mcp/internal/setup"
)

var version = "1.0.0"

const usage = `outline-mcp — Outline API MCP server

Usage:
  outline-mcp run [--config PATH]
  outline-mcp setup [--config PATH] [--harness auto|codex|claude|generic]
                    [--skill-dir DIR] [--print-config]
  outline-mcp version

run          Serve 74 tools and the history resource over stdin/stdout.
setup        Configure API-key authentication, MCP registration, and the skill.
--print-config  Print copyable MCP JSON without prompting or writing files.

The CLI does not execute Outline operations. Use the MCP tools from your harness.
Environment: OUTLINE_CONFIG, OUTLINE_BASE_URL, OUTLINE_API_KEY,
OUTLINE_HISTORY, OUTLINE_HISTORY_LIMIT, OUTLINE_TIMEOUT_SECONDS.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := execute(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "outline-mcp:", err)
		os.Exit(1)
	}
}

func execute(ctx context.Context, args []string, out, errOut io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(out, usage)
		return nil
	}
	if args[0] == "version" || args[0] == "--version" {
		if len(args) != 1 {
			return fmt.Errorf("version takes no arguments")
		}
		fmt.Fprintln(out, "outline-mcp", version)
		return nil
	}
	if args[0] != "run" && args[0] != "setup" {
		return fmt.Errorf("unknown command %q; only setup and run are supported (see --help)", args[0])
	}
	defaultPath, err := config.DefaultPath()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(errOut)
	path := fs.String("config", defaultPath, "Path to Outline MCP configuration")
	var options setup.Options
	if args[0] == "setup" {
		fs.StringVar(&options.Harness, "harness", "auto", "auto, codex, claude, or generic")
		fs.StringVar(&options.SkillDir, "skill-dir", "", "Skill installation directory (contains SKILL.md)")
		fs.BoolVar(&options.PrintConfig, "print-config", false, "Print MCP JSON; no prompts or writes")
	}
	if err = fs.Parse(args[1:]); errors.Is(err, flag.ErrHelp) {
		return nil
	} else if err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if args[0] == "setup" {
		options.ConfigPath = *path
		options.Executable, err = os.Executable()
		if err != nil {
			return err
		}
		return setup.Interactive(ctx, options)
	}
	c, err := config.Load(*path)
	if err != nil {
		return err
	}
	server, err := app.NewServer(c, version)
	if err != nil {
		return err
	}
	return server.Run(ctx, &mcp.StdioTransport{})
}
