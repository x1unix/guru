package cover

import (
	"fmt"
	"github.com/go-gilbert/gilbert-sdk"
	"os"
	"os/exec"
	"strings"

	"github.com/go-gilbert/gilbert/plugins/builtin/cover/profile"
	"github.com/go-gilbert/gilbert/tools/shell"
)

type plugin struct {
	scope     sdk.ScopeAccessor
	params    params
	coverFile *os.File
	log       sdk.Logger
	alive     bool
}

func (p *plugin) Call(ctx sdk.JobContextAccessor, r sdk.JobRunner) (err error) {
	defer p.clean()
	cmd, err := p.createCoverCommand(ctx)
	if err != nil {
		return err
	}

	ctx.Log().Debugf("cover command: '%s'", strings.Join(cmd.Args, " "))
	if err = cmd.Start(); err != nil {
		return fmt.Errorf("failed to start cover tool, %s", err)
	}

	if err = cmd.Wait(); err != nil {
		return shell.FormatExitError(err)
	}

	if !p.alive {
		return nil
	}

	pkgs, err := profile.ConvertProfiles(p.coverFile.Name())
	if err != nil {
		return fmt.Errorf("failed to parse cover profile file, %s", err)
	}

	// TODO: find a better approach to stop on cancel
	if !p.alive {
		return nil
	}

	// Check coverage
	report := profile.Create(*pkgs)
	if err := report.CheckCoverage(p.params.Threshold); err != nil {
		p.printReport(&report)
		return err
	}

	if p.params.Report {
		p.printReport(&report)
	}

	return nil
}

func (p *plugin) printReport(r *profile.Report) {
	p.log.Info("Coverage report:")
	var str string
	if p.params.FullReport {
		str = r.FormatFull()
	} else {
		str = r.FormatSimple()
	}

	_, _ = p.log.Write([]byte(str))
	p.log.Infof("Total coverage: %.2f%%", r.Percentage())
}

func (p *plugin) clean() {
	if !p.alive {
		return
	}

	p.alive = false
	fname := p.coverFile.Name()
	if err := os.Remove(fname); err != nil {
		p.log.Debugf("failed to remove cover file '%s': %s", fname, err)
		return
	}

	p.log.Debugf("removed cover file '%s'", fname)
}

func (p *plugin) createCoverCommand(ctx sdk.JobContextAccessor) (*exec.Cmd, error) {
	// pass package names as is, since '-coverpkg' doesn't recognise them in CSV format (go 1.11+)
	args := make([]string, 0, len(p.params.Packages)+toolArgsPrefixSize)
	args = append(args, "test", "-coverprofile="+p.coverFile.Name())

	for _, pkg := range p.params.Packages {
		val, err := p.scope.ExpandVariables(pkg)
		if err != nil {
			return nil, err
		}

		args = append(args, val)
	}

	cmd := exec.CommandContext(ctx.Context(), "go", args...)
	cmd.Dir = p.scope.Environment().ProjectDirectory
	cmd.Stderr = ctx.Log().ErrorWriter()
	return cmd, nil
}

func (p *plugin) Cancel(ctx sdk.JobContextAccessor) error {
	p.clean()
	return nil
}
