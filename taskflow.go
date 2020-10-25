package taskflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

var (
	ErrTaskNotRegistered = errors.New("task provided but not registered")
	ErrTaskFail          = errors.New("FAIL")
)

type Taskflow struct {
	Verbose bool
	Output  io.Writer

	tasks map[string]Task
}

type RegisteredTask struct {
	name string
}

func (f *Taskflow) Register(task Task) (RegisteredTask, error) {
	// validate
	if task.Name == "" {
		return RegisteredTask{}, errors.New("task name cannot be empty")
	}
	if f.isRegistered(task.Name) {
		return RegisteredTask{}, fmt.Errorf("%s task was already registered", task.Name)
	}
	for _, dep := range task.Dependencies {
		if !f.isRegistered(dep.name) {
			return RegisteredTask{}, fmt.Errorf("invalid dependency %s", dep.name)
		}
	}

	f.tasks[task.Name] = task
	return RegisteredTask{name: task.Name}, nil
}

func (f *Taskflow) MustRegister(task Task) RegisteredTask {
	dep, err := f.Register(task)
	if err != nil {
		panic(err)
	}
	return dep
}

func (f *Taskflow) Run(ctx context.Context, taskNames ...string) error {
	// validate
	for _, name := range taskNames {
		if !f.isRegistered(name) {
			return ErrTaskNotRegistered
		}
	}

	// recursive run
	executedTasks := map[string]bool{}
	for _, name := range taskNames {
		if err := f.run(ctx, name, executedTasks); err != nil {
			return err
		}
	}
	return nil
}

func (f *Taskflow) MustRun(ctx context.Context, taskNames ...string) {
	err := f.Run(ctx, taskNames...)
	if err != nil {
		panic(err)
	}
}

func (f *Taskflow) run(ctx context.Context, name string, executed map[string]bool) error {
	task := f.tasks[name]
	if executed[name] {
		return nil
	}
	for _, dep := range task.Dependencies {
		if err := f.run(ctx, dep.name, executed); err != nil {
			return err
		}
	}
	if !f.runTask(ctx, task) {
		return ErrTaskFail
	}
	executed[name] = true
	return nil
}

func (f *Taskflow) runTask(ctx context.Context, task Task) bool {
	if task.Command == nil {
		return true
	}

	// TODO:
	// 1. Handle cancelation via ctx. New state? Check how go test does it.
	// 2. Handle writer streaming for verbose mode.
	// 3. Handle panics.
	sb := &strings.Builder{}

	sb.WriteString(reportTaskStart(task.Name))

	runner := Runner{
		Ctx:     ctx,
		Name:    task.Name,
		Command: task.Command,
		Out:     sb,
	}
	tf := runner.Run()

	switch {
	default:
		sb.WriteString(reportTaskEnd("PASS", task.Name, tf.duration))
	case tf.failed:
		sb.WriteString(reportTaskEnd("FAIL", task.Name, tf.duration))
	case tf.skipped:
		sb.WriteString(reportTaskEnd("SKIP", task.Name, tf.duration))
	}

	if f.Verbose || tf.failed {
		if _, err := io.Copy(f.output(), strings.NewReader(sb.String())); err != nil {
			panic(err)
		}
	}

	return !tf.failed
}

func (f *Taskflow) isRegistered(name string) bool {
	if f.tasks == nil {
		f.tasks = map[string]Task{}
	}
	_, ok := f.tasks[name]
	return ok
}

func (f *Taskflow) output() io.Writer {
	if f.Output == nil {
		return os.Stdout
	}
	return f.Output
}

func reportTaskStart(taskName string) string {
	return fmt.Sprintf("===== TASK  %s\n", taskName)
}

func reportTaskEnd(status string, taskName string, d time.Duration) string {
	return fmt.Sprintf("----- %s: %s (%.2fs)\n", status, taskName, d.Seconds())
}
