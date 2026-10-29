// Package setup implements the interactive installer. It never calls Outline.
package setup

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"
	"outline-mcp/internal/config"
	"outline-mcp/internal/fileutil"
	"outline-mcp/skills"
)

type Options struct {
	ConfigPath, Executable, Harness, SkillDir string
	PrintConfig                               bool
}
type ServerConfig struct {
	Type    string   `json:"type"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

func MCPConfig(executable, path string) ServerConfig {
	return ServerConfig{"stdio", executable, []string{"run", "--config", path}}
}
func JSON(executable, path string) []byte {
	b, _ := json.MarshalIndent(map[string]any{"mcpServers": map[string]any{"outline": MCPConfig(executable, path)}}, "", "  ")
	return append(b, '\n')
}

// Installer dependencies are injected so tests never change real harness settings.
type Installer struct {
	In         io.Reader
	Out        io.Writer
	Home       string
	LookPath   func(string) (string, error)
	RunCommand func(context.Context, string, ...string) error
	Secret     func() (string, error)
}

func Interactive(ctx context.Context, o Options) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	i := Installer{In: os.Stdin, Out: os.Stdout, Home: home, LookPath: exec.LookPath, RunCommand: func(ctx context.Context, name string, args ...string) error {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		i.Secret = func() (string, error) {
			b, e := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stdout)
			return string(b), e
		}
	}
	return i.Run(ctx, o)
}

func (i Installer) Run(ctx context.Context, o Options) error {
	var err error
	o.ConfigPath, err = filepath.Abs(o.ConfigPath)
	if err != nil {
		return err
	}
	o.Executable, err = filepath.Abs(o.Executable)
	if err != nil {
		return err
	}
	if o.PrintConfig {
		_, err = i.Out.Write(JSON(o.Executable, o.ConfigPath))
		return err
	}
	if o.Harness == "" {
		o.Harness = "auto"
	}
	switch o.Harness {
	case "auto", "codex", "claude", "generic":
	default:
		return fmt.Errorf("harness must be auto, codex, claude, or generic")
	}
	reader := bufio.NewReader(i.In)
	ask := func(label, def string) (string, error) {
		fmt.Fprint(i.Out, label)
		if def != "" {
			fmt.Fprintf(i.Out, " [%s]", def)
		}
		fmt.Fprint(i.Out, ": ")
		s, e := reader.ReadString('\n')
		if e != nil {
			return "", fmt.Errorf("setup input ended; no implicit approval: %w", e)
		}
		s = strings.TrimSpace(s)
		if s == "" {
			s = def
		}
		return s, nil
	}
	yes := func(label string) (bool, error) {
		s, e := ask(label+" (y/N)", "n")
		return strings.EqualFold(s, "y") || strings.EqualFold(s, "yes"), e
	}
	c, err := config.Load(o.ConfigPath)
	if err != nil {
		return err
	}
	fmt.Fprintln(i.Out, "Outline MCP setup — API key only. No requests will be sent to Outline.")
	c.BaseURL, err = ask("Outline base URL", c.BaseURL)
	if err != nil {
		return err
	}
	if _, err = config.NormalizeURL(c.BaseURL); err != nil {
		return err
	}
	fmt.Fprintln(i.Out, "The key is stored in the local config (0600 on Unix; Windows uses directory ACLs). MCP JSON contains only its path.")
	keyLabel := "API key"
	if c.APIKey != "" {
		keyLabel += " (blank keeps the current key)"
	}
	var key string
	if i.Secret != nil {
		fmt.Fprint(i.Out, keyLabel+": ")
		key, err = i.Secret()
	} else {
		fmt.Fprintln(i.Out, "Input is not a terminal; secret input cannot be hidden.")
		key, err = ask(keyLabel, "")
	}
	if err != nil {
		return err
	}
	if strings.TrimSpace(key) != "" {
		c.APIKey = strings.TrimSpace(key)
	}
	c.History, err = yes("Enable in-memory action history (discarded when the server exits)")
	if err != nil {
		return err
	}
	if err = c.Validate(); err != nil {
		return err
	}
	if o.Harness == "auto" {
		detected := []string{}
		for _, name := range []string{"codex", "claude"} {
			if _, e := i.LookPath(name); e == nil {
				detected = append(detected, name)
			}
		}
		choice := "generic"
		if len(detected) == 1 {
			choice = detected[0]
		}
		if len(detected) > 0 {
			fmt.Fprintf(i.Out, "Detected on PATH: %s\n", strings.Join(detected, ", "))
		} else {
			fmt.Fprintln(i.Out, "No supported harness CLI detected. Generic setup prints copyable MCP JSON.")
		}
		o.Harness, err = ask("Harness (codex/claude/generic)", choice)
		if err != nil {
			return err
		}
	}
	switch o.Harness {
	case "codex", "claude":
		if _, err = i.LookPath(o.Harness); err != nil {
			fmt.Fprintf(i.Out, "%s CLI unavailable; using generic MCP JSON.\n", o.Harness)
			o.Harness = "generic"
		}
	case "generic":
	default:
		return fmt.Errorf("unknown harness %q", o.Harness)
	}
	if o.SkillDir == "" {
		switch o.Harness {
		case "codex":
			o.SkillDir = filepath.Join(i.Home, ".agents", "skills", "outline")
		case "claude":
			o.SkillDir = filepath.Join(i.Home, ".claude", "skills", "outline")
		default:
			o.SkillDir, err = ask("Skill destination directory (blank skips installation)", "")
			if err != nil {
				return err
			}
		}
	}
	if o.SkillDir != "" {
		o.SkillDir, err = filepath.Abs(o.SkillDir)
		if err != nil {
			return err
		}
	}
	fmt.Fprintf(i.Out, "\nConfig: %s\nExecutable: %s\nHarness: %s\n", o.ConfigPath, o.Executable, o.Harness)
	if o.SkillDir != "" {
		fmt.Fprintf(i.Out, "Skill: %s\n", filepath.Join(o.SkillDir, "SKILL.md"))
	}
	if o.Harness != "generic" {
		fmt.Fprintln(i.Out, "The harness CLI will register/replace the user-level MCP server named outline. Other servers are left to the harness CLI to preserve.")
	}
	approved, err := yes("Apply this setup")
	if err != nil {
		return err
	}
	if !approved {
		fmt.Fprintln(i.Out, "Cancelled; nothing written.")
		return nil
	}
	if err = backup(o.ConfigPath); err != nil {
		return err
	}
	if err = config.Save(o.ConfigPath, c); err != nil {
		return err
	}
	fmt.Fprintf(i.Out, "Saved %s\n", o.ConfigPath)
	if o.SkillDir != "" {
		path := filepath.Join(o.SkillDir, "SKILL.md")
		if err = backup(path); err != nil {
			return fmt.Errorf("config saved; skill backup failed: %w", err)
		}
		if err = fileutil.Write(path, skills.Outline, 0644); err != nil {
			return fmt.Errorf("config saved; skill installation failed: %w", err)
		}
		fmt.Fprintf(i.Out, "Installed %s\n", path)
	}
	if o.Harness != "generic" {
		args := []string{}
		switch o.Harness {
		case "codex":
			args = []string{"mcp", "add", "outline", "--", o.Executable, "run", "--config", o.ConfigPath}
		case "claude":
			b, _ := json.Marshal(MCPConfig(o.Executable, o.ConfigPath))
			args = []string{"mcp", "add-json", "--scope", "user", "outline", string(b)}
		}
		commandCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if err = i.RunCommand(commandCtx, o.Harness, args...); err != nil {
			fmt.Fprintln(i.Out, "Config and skill were saved, but harness registration failed. Manual MCP JSON:")
			i.Out.Write(JSON(o.Executable, o.ConfigPath))
			return fmt.Errorf("%s registration failed: %w", o.Harness, err)
		}
	}
	fmt.Fprintln(i.Out, "\nMCP JSON (copy the outline entry into your client's configuration):")
	i.Out.Write(JSON(o.Executable, o.ConfigPath))
	fmt.Fprintln(i.Out, "Restart/reload your harness to discover the server and skill. Keep the executable at this path.")
	return nil
}

func backup(path string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if bytes.Equal(data, skills.Outline) {
		return nil
	}
	return fileutil.Write(path+".bak-"+time.Now().UTC().Format("20060102T150405.000000000"), data, 0600)
}
