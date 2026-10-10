package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/activity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/evaluate"
)

// People's tasks are run in the same evaluation directory as agents'
// (docs/EVALUATING_PEOPLE.md, "Measure"), on the same frozen build, under
// tasks/ rather than runs/:
//
//	tasks/NNN/   one run: its vault, its people's trace (people.jsonl),
//	             server.log, run.json and, once scored, score.json

const evalUXUsage = `
  cartograph eval ux-serve <dir> -task <file>                serve a fresh run of a person's task, traced; print its brief
  cartograph eval ux-score <dir> <run> -task <file>          score the run from the people's trace
  cartograph eval ux-status <dir> -task <file>               every run, and the streak against the bar
  cartograph eval ux-stop <dir> [<run>]                      stop a run's server, or every run's`

// uxScore is a person's run judged.
type uxScore struct {
	Checks []activity.Check `json:"checks"`
	Pass   bool             `json:"pass"`
}

func evalUXServeCmd(args []string) error {
	fs := flag.NewFlagSet("eval ux-serve", flag.ContinueOnError)
	taskPath := fs.String("task", "", "the task file: the brief, an optional seed vault, and the bar")
	dir, err := parseWithDir(fs, args)
	if err != nil {
		return err
	}
	task, err := readTaskBrief(*taskPath)
	if err != nil {
		return err
	}
	var b evalBuild
	if err := readJSON(filepath.Join(dir, "build.json"), &b); err != nil {
		return fmt.Errorf("no frozen build in %s: run just eval-build first: %w", dir, err)
	}
	runs, err := evalTaskRuns(dir)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%03d", len(runs)+1)
	rd := filepath.Join(dir, "tasks", name)
	vault := filepath.Join(rd, "vault")
	if err := os.MkdirAll(vault, 0o755); err != nil {
		return err
	}
	if task.Seed != "" {
		seed := task.Seed
		if !filepath.IsAbs(seed) {
			seed = filepath.Join(filepath.Dir(*taskPath), seed)
		}
		if err := copyDir(seed, vault); err != nil {
			return fmt.Errorf("seed the vault: %w", err)
		}
	}
	port, err := freePort()
	if err != nil {
		return err
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	logf, err := os.Create(filepath.Join(rd, "server.log"))
	if err != nil {
		return err
	}
	defer logf.Close()
	cmd := exec.Command(b.binary(), "serve", vault, "-addr", addr)
	cmd.Env = append(os.Environ(), "CARTOGRAPH_UI_TRACE="+filepath.Join(rd, "people.jsonl"))
	cmd.Stdout, cmd.Stderr = logf, logf
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := waitUp("http://"+addr+"/", 30*time.Second); err != nil {
		_ = cmd.Process.Kill()
		return err
	}
	run := evalRun{Name: name, Commit: b.Commit, Endpoint: "http://" + addr + "/", PID: cmd.Process.Pid, Started: time.Now().UTC()}
	if err := writeJSON(filepath.Join(rd, "run.json"), run); err != nil {
		return err
	}
	fmt.Printf("task run %s on build %s, serving %s\n\nThe brief, word for word:\n\n%s\n", name, short(b.Commit), run.Endpoint, task.Brief)
	return nil
}

func evalUXScoreCmd(args []string) error {
	fs := flag.NewFlagSet("eval ux-score", flag.ContinueOnError)
	taskPath := fs.String("task", "", "the task file")
	if err := fs.Parse(reorder(args)); err != nil {
		return err
	}
	if fs.NArg() < 2 || *taskPath == "" {
		return errors.New(evalUsage)
	}
	dir, name := fs.Arg(0), fs.Arg(1)
	task, err := readTaskBrief(*taskPath)
	if err != nil {
		return err
	}
	rd := filepath.Join(dir, "tasks", name)
	acts, err := readActs(context.Background(), []string{filepath.Join(rd, "people.jsonl")}, activity.Query{})
	if err != nil {
		return fmt.Errorf("run %s has no people's trace: %w", name, err)
	}
	flows, err := activity.Flows()
	if err != nil {
		return err
	}
	budget, err := activity.Budget()
	if err != nil {
		return err
	}
	r := activity.Analyse(acts, flows, activity.Options{Budget: budget})
	s := uxScore{Checks: activity.ScoreRun(r, task.Bar), Pass: true}
	for _, c := range s.Checks {
		s.Pass = s.Pass && c.Pass
	}
	if err := writeJSON(filepath.Join(rd, "score.json"), s); err != nil {
		return err
	}
	passed := 0
	for _, c := range s.Checks {
		mark := "FAIL"
		if c.Pass {
			mark, passed = "PASS", passed+1
		}
		fmt.Printf("%s  %s: %s\n", mark, c.Name, c.Why)
	}
	fmt.Printf("task run %s: %d/%d\n\n", name, passed, len(s.Checks))
	printUX(os.Stdout, r)
	return nil
}

func evalUXStatusCmd(args []string) error {
	fs := flag.NewFlagSet("eval ux-status", flag.ContinueOnError)
	taskPath := fs.String("task", "", "the task file")
	dir, err := parseWithDir(fs, args)
	if err != nil {
		return err
	}
	bar, name := 3, ""
	if *taskPath != "" {
		t, err := readTaskBrief(*taskPath)
		if err != nil {
			return err
		}
		bar, name = t.Runs, t.Name
	}
	runs, err := evalTaskRuns(dir)
	if err != nil {
		return err
	}
	var scored []evaluate.Run
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "run\tbuild\tchecks")
	for _, r := range runs {
		er := evaluate.Run{Name: r.Name, Commit: r.Commit}
		var s uxScore
		if err := readJSON(filepath.Join(dir, "tasks", r.Name, "score.json"), &s); err == nil {
			// The streak reads whether each run passed; its checks are this
			// run's own.
			er.Score = &evaluate.Score{Pass: s.Pass}
			passed := 0
			for _, c := range s.Checks {
				if c.Pass {
					passed++
				}
			}
			fmt.Fprintf(w, "%s\t%s\t%d/%d\n", r.Name, short(r.Commit), passed, len(s.Checks))
		} else {
			fmt.Fprintf(w, "%s\t%s\tnot scored\n", r.Name, short(r.Commit))
		}
		scored = append(scored, er)
	}
	_ = w.Flush()
	st := evaluate.StreakOf(scored, bar, 0)
	if name != "" {
		fmt.Printf("\n%s: ", name)
	} else {
		fmt.Println()
	}
	fmt.Printf("%d of %d in a row on build %s", st.InARow, st.Bar, short(st.Commit))
	if st.Closed {
		fmt.Println(": the bar is met.")
	} else {
		fmt.Println(": not met yet.")
	}
	return nil
}

func evalUXStopCmd(args []string) error {
	if len(args) == 0 {
		return errors.New(evalUsage)
	}
	runs, err := evalTaskRuns(args[0])
	if err != nil {
		return err
	}
	for _, r := range runs {
		if len(args) > 1 && r.Name != args[1] {
			continue
		}
		if p, err := os.FindProcess(r.PID); err == nil && stop(p) == nil {
			fmt.Printf("stopped task run %s (pid %d)\n", r.Name, r.PID)
		}
	}
	return nil
}

func evalTaskRuns(dir string) ([]evalRun, error) {
	entries, err := os.ReadDir(filepath.Join(dir, "tasks"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []evalRun
	for _, e := range entries {
		var r evalRun
		if e.IsDir() && readJSON(filepath.Join(dir, "tasks", e.Name(), "run.json"), &r) == nil {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// copyDir copies a seed vault's files into a run's fresh vault.
func copyDir(from, to string) error {
	return filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(to, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		return copyFile(path, dst, 0o644)
	})
}

// readTaskBrief reads a task file kept beside an evaluation directory.
func readTaskBrief(path string) (activity.TaskBrief, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return activity.TaskBrief{}, err
	}
	t, err := activity.ParseTaskBrief(b)
	if err != nil {
		return t, fmt.Errorf("%s: %w", path, err)
	}
	return t, nil
}
