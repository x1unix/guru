package runner

import (
	"context"
	"fmt"
	"github.com/go-gilbert/gilbert/actions"
	"strings"
	"time"

	"github.com/go-gilbert/gilbert-sdk"
	"github.com/go-gilbert/gilbert/manifest"
	"github.com/go-gilbert/gilbert/scope"
	"github.com/go-gilbert/gilbert/support/shell"

	"github.com/go-gilbert/gilbert/runner/job"
)

var errNoTaskHandler = fmt.Errorf("no task handler defined, please define task handler in 'plugin' or 'mixin' paramerer")

// TaskRunner runs tasks
type TaskRunner struct {
	manifest         *manifest.Manifest
	CurrentDirectory string
	log              sdk.Logger
	subLogger        sdk.Logger
	context          context.Context
	cancelFn         context.CancelFunc
}

// SetContext sets execution context
func (t *TaskRunner) SetContext(ctx context.Context, fn context.CancelFunc) {
	t.context = ctx
	t.cancelFn = fn
}

// ActionByName returns action handler constructor
func (t *TaskRunner) ActionByName(actionName string) (p sdk.HandlerFactory, err error) {
	return actions.GetHandler(actionName)
}

// Stop stops task runner
func (t *TaskRunner) Stop() {
	if t.cancelFn != nil {
		t.cancelFn()
	}
}

// Run executes task by name.
//
// "vars" parameter is optional and allows to override job scope values.
func (t *TaskRunner) Run(taskName string, vars sdk.Vars) (err error) {
	task, ok := t.manifest.Tasks[taskName]
	if !ok {
		return fmt.Errorf("task '%s' doesn't exists", taskName)
	}

	t.log.Logf("Running task '%s'...", taskName)
	steps := len(task)

	sl := t.subLogger.SubLogger()
	if t.context == nil {
		t.log.Warn("Warning: task context was not set")
		t.context, t.cancelFn = context.WithCancel(context.Background())
	}

	// Set waitgroup and buff channel for async jobs.
	var tracker *asyncJobTracker
	asyncJobsCount := task.AsyncJobsCount()
	if asyncJobsCount > 0 {
		t.subLogger.Debugf("runner: %d async jobs in task", asyncJobsCount)
		tracker = newAsyncJobTracker(t.context, t.subLogger, asyncJobsCount)
		go tracker.trackAsyncJobs()

		defer func() {
			// Wait for unfinished async tasks
			// and collect results from async jobs
			t.subLogger.Logf("Waiting for %d async job(s) to complete", asyncJobsCount)
			if asyncErr := tracker.wait(); asyncErr != nil {
				if err == nil {
					// Report error only if no previous errors.
					// P.S - it's okay since all async errors were logged previously
					err = fmt.Errorf("task '%s' returned error in async job: %s", taskName, asyncErr)
				}
			}
		}()
	}

	for jobIndex, j := range task {
		currentStep := jobIndex + 1
		descr := j.FormatDescription()
		if steps > 1 {
			// show total steps count only if more than one step provided
			t.subLogger.Infof("- [%d/%d] %s", currentStep, steps, descr)
		} else {
			t.subLogger.Infof("- %s", descr)
		}
		var err error
		ctx := job.NewRunContext(t.context, vars, sl)

		if j.Async {
			tracker.decorateJobContext(ctx)
			go t.handleJob(j, ctx)
			continue
		}

		if err = t.startJobAndWait(j, ctx); err != nil {
			return fmt.Errorf("task '%s' returned an error on step %d: %v", taskName, currentStep, err)
		}
	}

	return err
}

// RunTask starts sub-task by name
//
// Returns an error if task is not defined or returned an error
func (t *TaskRunner) RunTask(taskName string, ctx sdk.JobContextAccessor, scope sdk.ScopeAccessor) error {
	task, ok := t.manifest.Tasks[taskName]
	if !ok {
		return fmt.Errorf("task '%s' doesn't exists", taskName)
	}

	// Create a task copy with injected local variables from scope
	// if scope has some variables
	locals := scope.Vars()
	if len(locals) > 0 {
		task = task.Clone(locals)
	}

	ctx.Log().Debugf("runner: start sub-task '%s'", taskName)
	if err := t.runSubTask(task, scope, ctx); err != nil {
		ctx.Log().Debugf("runner: task '%s' returned an error '%s'", taskName, err.Error())
		return err
	}

	return nil
}

// RunJob starts job in separate goroutine.
//
// Use ctx.Error channel to track job result and ctx.Cancel() to cancel it.
func (t *TaskRunner) RunJob(j sdk.Job, ctx sdk.JobContextAccessor) {
	go t.handleJob(j, ctx)
}

func (t *TaskRunner) startJobAndWait(job sdk.Job, ctx sdk.JobContextAccessor) error {
	go t.handleJob(job, ctx)
	// All child jobs (except async jobs) inherit parent job channel,
	// so we should close channel only if parent job was finished.
	if !ctx.IsChild() {
		defer close(ctx.Errors())
	}

	err, ok := <-ctx.Errors()
	if !ok {
		ctx.Log().Debug("runner: failed to read data from result channel!!!")
		return nil
	}

	return err
}

// handleJob handles specified job
func (t *TaskRunner) handleJob(j sdk.Job, ctx sdk.JobContextAccessor) {
	s := scope.CreateScope(t.CurrentDirectory, j.Vars).
		AppendGlobals(t.manifest.Vars).
		AppendVariables(ctx.Vars())

	// check if job should be run
	if !t.shouldRunJob(j, s) {
		ctx.Log().Info("step was skipped")
		ctx.Success()
		return
	}

	// Wait if necessary
	if j.Delay > 0 {
		ctx.Log().Debugf("runner: job delay defined, waiting %dms...", j.Delay)
		time.Sleep(j.Delay.ToDuration())
	}

	if j.Deadline > 0 {
		// Add timeout if requested
		ttl := j.Deadline.ToDuration()
		ctx.Timeout(ttl)
	}

	execType := j.Type()
	switch execType {
	case sdk.ExecAction:
		t.handleActionCall(ctx, j, s)
	case sdk.ExecMixin:
		t.handleMixinCall(ctx, j, s)
	case sdk.ExecTask:
		t.handleSubTaskCall(ctx, j, s)
	default:
		ctx.Result(errNoTaskHandler)
	}
}

func (t *TaskRunner) handleSubTaskCall(ctx sdk.JobContextAccessor, j sdk.Job, s sdk.ScopeAccessor) {
	err := t.RunTask(j.TaskName, ctx, s)
	ctx.Result(err)
}

func (t *TaskRunner) handleActionCall(ctx sdk.JobContextAccessor, j sdk.Job, s sdk.ScopeAccessor) {
	factory, err := t.ActionByName(j.ActionName)
	if err != nil {
		ctx.Result(err)
		return
	}

	actionHandler, err := factory(s, j.Params)
	if err != nil {
		ctx.Result(fmt.Errorf("failed to create action handler instance of '%s': %s", j.ActionName, err))
		return
	}

	// Handle stop event
	// Event may arrive on SIGKILL or when timeout reached
	go func() {
		<-ctx.Context().Done()
		ctx.Log().Debugf("runner: sent stop signal to '%s' action handler", j.ActionName)
		ctx.Result(actionHandler.Cancel(ctx))
	}()

	// Call actionHandler and send result
	err = actionHandler.Call(ctx, t)
	ctx.Result(err)
}

// handleMixinCall constructs a task from job with mixin and runs it
//
// requires subLogger instance to create cascade logging output
func (t *TaskRunner) handleMixinCall(ctx sdk.JobContextAccessor, j sdk.Job, s sdk.ScopeAccessor) {
	mx, ok := t.manifest.Mixins[j.MixinName]
	if !ok {
		ctx.Result(fmt.Errorf("mixin '%s' doesn't exists", j.MixinName))
		return
	}

	// Create a task from mixin and job params
	ctx.Log().Debugf("runner: create sub-task from mixin '%s'", j.MixinName)
	task := mx.ToTask(s.Vars())
	if err := t.runSubTask(task, s, ctx); err != nil {
		ctx.Result(err)
		return
	}

	ctx.Success()
}

// runSubTask used to run sub-tasks created by parent job
//
// parentCtx used to expand task base properties (like description, etc.)
//
// subLogger used to create stack of log lines
func (t *TaskRunner) runSubTask(task manifest.Task, parentScope sdk.ScopeAccessor, parentCtx sdk.JobContextAccessor) (err error) {
	// FIXME: drop copy-paste from Run
	steps := len(task)

	// Set waitgroup and buff channel for async jobs.
	var tracker *asyncJobTracker
	asyncJobsCount := task.AsyncJobsCount()
	if asyncJobsCount > 0 {
		parentCtx.Log().Debugf("runner: %d async jobs in sub-task", asyncJobsCount)
		tracker = newAsyncJobTracker(parentCtx.Context(), parentCtx.Log(), asyncJobsCount)
		go tracker.trackAsyncJobs()

		defer func() {
			// Wait for unfinished async tasks
			// and collect results from async jobs
			t.subLogger.Logf("Waiting for %d async job(s) to complete", asyncJobsCount)
			if asyncErr := tracker.wait(); asyncErr != nil {

				if err == nil {
					// Report error only if no previous errors.
					// P.S - it's okay since all async errors were logged previously
					err = fmt.Errorf("async job returned error - %s", asyncErr)
				}
			}
		}()
	}

	for jobIndex, j := range task {
		currentStep := jobIndex + 1

		// sub task label can contain template expressions (e.g. mixin step description)
		// so we should try to parse it
		descr := j.FormatDescription()
		if parsed, perr := parentScope.ExpandVariables(descr); perr != nil {
			parentCtx.Log().Errorf("description parse error: %s", perr)
		} else {
			descr = parsed
		}

		if steps > 1 {
			// show total steps count only if more than one step provided
			parentCtx.Log().Infof("- [%d/%d] %s", currentStep, steps, descr)
		} else {
			parentCtx.Log().Infof("- %s", descr)
		}

		ctx := parentCtx.ChildContext()
		if j.Async {
			tracker.decorateJobContext(ctx)
			go t.handleJob(j, ctx)
			continue
		}

		if err = t.startJobAndWait(j, ctx); err != nil {
			return fmt.Errorf("%s (sub-task step %d)", err, currentStep)
		}
	}

	return err
}

func (t *TaskRunner) shouldRunJob(job sdk.Job, scp sdk.ScopeAccessor) bool {
	condCmd := strings.TrimSpace(job.Condition)
	if condCmd == "" {
		return true
	}

	l := t.subLogger.SubLogger()
	condCmd, err := scp.ExpandVariables(condCmd)
	if err != nil {
		l.Error(err.Error())
		l.Warn("Failed to parse value inside 'if' expression, job will be skipped")
		return false
	}
	cmd := shell.PrepareCommand(condCmd)

	l.Debugf("runner: assert command: '%s'", condCmd)

	// Return false if command failed to start or returned bad exit code
	if err := cmd.Start(); err != nil {
		return false
	}

	if err := cmd.Wait(); err != nil {
		return false
	}

	return true
}

// NewTaskRunner creates a new task runner instance
func NewTaskRunner(man *manifest.Manifest, cwd string, writer sdk.Logger) *TaskRunner {
	t := &TaskRunner{
		manifest:         man,
		CurrentDirectory: cwd,
		log:              writer,
		subLogger:        writer.SubLogger(),
	}

	return t
}
