package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/CristhianRudolf7/excalibur-agent/internal/cli"
)

// serveMCP runs the Section 3.6 delegation server over stdio.
func serveMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	rest, err := parseCmd(fs, args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return &cli.CodedError{Code: cli.ExitError, Err: fmt.Errorf("usage: excalibur mcp")}
	}
	srv := &cli.MCPServer{Stdin: os.Stdin, Stdout: os.Stdout}
	return srv.Serve(noSignalContext())
}

// installMCP implements `excalibur mcp install`.
func installMCP(args []string) error {
	fs := flag.NewFlagSet("mcp install", flag.ContinueOnError)
	client := fs.String("client", "", "client: codex|claude-code|opencode|muse-code")
	printOnly := fs.Bool("print", false, "print the fragment instead of writing it")
	withSkill := fs.Bool("skill", false, "also install the excalibur-delegate skill")
	if _, err := parseCmd(fs, args); err != nil {
		return err
	}
	if *client == "" {
		return &cli.CodedError{Code: cli.ExitError, Err: fmt.Errorf("usage: excalibur mcp install --client <codex|claude-code|opencode|muse-code> [--print] [--skill]")}
	}
	return cli.InstallMCP(*client, *printOnly, *withSkill, os.Stdout)
}

// serveACP runs the Agent Client Protocol adapter over stdio.
func serveACP(args []string) error {
	fs := flag.NewFlagSet("acp", flag.ContinueOnError)
	profile := fs.String("profile", "eval", "execution profile (default: eval)")
	project := fs.String("project", "", "project directory scope")
	if _, err := parseCmd(fs, args); err != nil {
		return err
	}
	p, err := cli.ParseProfile(*profile)
	if err != nil {
		return &cli.CodedError{Code: cli.ExitError, Err: err}
	}
	srv := &cli.ACPServer{Stdin: os.Stdin, Stdout: os.Stdout, Profile: p, ProjectDir: *project}
	return srv.Serve(noSignalContext())
}
