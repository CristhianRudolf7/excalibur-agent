package main

import (
	"flag"
	"reflect"
	"testing"
)

func TestParseCmd(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantArgs    []string
		wantName    string
		wantTimeout int
		wantBool    bool
	}{
		{name: "flag after positional", args: []string{"T45", "--timeout", "300"}, wantArgs: []string{"T45"}, wantName: "default", wantTimeout: 300},
		{name: "negative value", args: []string{"--timeout", "-3", "request"}, wantArgs: []string{"request"}, wantName: "default", wantTimeout: -3},
		{name: "empty value does not consume positional", args: []string{"--name=", "request"}, wantArgs: []string{"request"}, wantName: ""},
		{name: "separator keeps flags positional", args: []string{"--verbose", "--", "--name", "request"}, wantArgs: []string{"--name", "request"}, wantName: "default", wantBool: true},
		{name: "single dash flag", args: []string{"-name", "request"}, wantArgs: []string{}, wantName: "request"},
		{name: "do not strip multiple leading dashes", args: []string{"---name", "request"}, wantArgs: []string{"---name", "request"}, wantName: "default"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := flag.NewFlagSet("test", flag.ContinueOnError)
			fs.SetOutput(nil)
			value := fs.String("name", "default", "")
			timeout := fs.Int("timeout", 0, "")
			verbose := fs.Bool("verbose", false, "")
			got, err := parseCmd(fs, tt.args)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.wantArgs) {
				t.Fatalf("args = %#v, want %#v", got, tt.wantArgs)
			}
			if *value != tt.wantName {
				t.Errorf("name = %q, want %q", *value, tt.wantName)
			}
			if *timeout != tt.wantTimeout {
				t.Errorf("timeout = %d, want %d", *timeout, tt.wantTimeout)
			}
			if *verbose != tt.wantBool {
				t.Errorf("verbose = %v, want %v", *verbose, tt.wantBool)
			}
		})
	}
}

func TestParseCmdMissingFlagValue(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(nil)
	fs.String("name", "", "")
	if _, err := parseCmd(fs, []string{"--name"}); err == nil {
		t.Fatal("expected missing value error")
	}
}

func TestParseCmdRecognizedFlagDoesNotBecomeValue(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(nil)
	fs.String("name", "default", "")
	fs.Bool("verbose", false, "")
	if _, err := parseCmd(fs, []string{"--name", "--verbose"}); err == nil {
		t.Fatal("expected missing value error for --name")
	}
}
