package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-gilbert/gilbert/support/fs"
	"github.com/go-gilbert/gilbert/v2/manifest"
	"github.com/spf13/cobra"
)

type CommandHandler = func(c *cobra.Command, args []string) error

var BinName = filepath.Base(os.Args[0])

const (
	DocURL       = "https://go-gilbert.github.io"
	SyntaxDocURL = DocURL + "/docs/syntax"
	TaskDocURL   = DocURL + "/docs/syntax/#tasks"
	CliDocURL    = DocURL + "/docs/commands/"
)

func FindManifestTask(taskName string) (*manifest.Manifest, *manifest.Task, error) {
	m, err := FindManifest()
	if err != nil {
		return nil, nil, err
	}

	t, ok := m.Tasks[taskName]
	if !ok {
		return nil, nil, fmt.Errorf(
			"no such task %[1]q.\n\n"+
				"Task %[1]q should be defined in file %[2]q to be able to run it.\n"+
				"See %[3]s for more information",
			taskName, m.FileName, TaskDocURL,
		)
	}

	return m, &t, nil
}

func FindManifest() (*manifest.Manifest, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %s", err)
	}

	manPath, found, err := fs.Lookup(manifest.DefaultFileName, wd, 3)
	if err != nil {
		return nil, fmt.Errorf("failed to find file %q: %s", manifest.DefaultFileName, err.Error())
	}

	if !found {
		return nil, fmt.Errorf(
			`file %q not found in project directory. Use "%s init" to create a new one`,
			manifest.DefaultFileName, BinName,
		)
	}

	// TODO: prepare context
	return manifest.FromFile(manPath, nil)
}

func WrapCobraCommand(h CommandHandler) func(*cobra.Command, []string) {
	return func(c *cobra.Command, args []string) {
		ExitWithError(h(c, args))
	}
}

func ExitWithErrorMessage(msg string, args ...interface{}) {
	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}
	_, _ = fmt.Fprintln(os.Stderr, "error: ", msg)
	os.Exit(1)
}

func ExitWithError(err error) {
	if err == nil {
		return
	}

	switch t := err.(type) {
	case *manifest.Error:
		// Don't show "error: " prefix for manifest syntax errors
		// as they have special formatting
		_, _ = fmt.Fprintln(
			os.Stderr,
			t.PrettyPrint(),
			"\nSee", SyntaxDocURL, "for file syntax information",
		)
	default:
		_, _ = fmt.Fprintln(os.Stderr, "error:", err.Error())
	}
	os.Exit(1)
}