package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/evaluate"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/trace"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/trace/jsonl"
)

// An evaluation directory holds one feature's runs (docs/EVALUATING.md):
//
//	build.json       the frozen build every counted run serves: the commit
//	                 and the Nix store path of the flake built at it
//	build            a link to that store path, which keeps it from
//	                 being collected
//	runs/NNN/        one run: its document, vault, work, trace, run.json
//	                 and, once scored, score.json
//
// It lives outside the repository, beside the criteria file.

type evalBuild struct {
	Commit string    `json:"commit"`
	Store  string    `json:"store"`
	Built  time.Time `json:"built"`
}

// binary is the frozen build's command, in the Nix store.
func (b evalBuild) binary() string { return filepath.Join(b.Store, "bin", "cartograph") }

type evalRun struct {
	Name     string    `json:"name"`
	Commit   string    `json:"commit"`
	Endpoint string    `json:"endpoint"`
	PID      int       `json:"pid"`
	Agent    string    `json:"agent"`
	Document string    `json:"document"`
	Started  time.Time `json:"started"`
}

// runEval judges a change agents use by DMAIC: freeze a build, serve
// fresh traced runs, give an agent a way to call the server, score each
// run from the server, and say where the streak stands.
func runEval(args []string) error {
	if len(args) == 0 {
		return errors.New(evalUsage)
	}
	switch args[0] {
	case "build":
		return evalBuildCmd(args[1:])
	case "serve":
		return evalServeCmd(args[1:])
	case "call":
		return evalCallCmd(args[1:])
	case "score":
		return evalScoreCmd(args[1:])
	case "status":
		return evalStatusCmd(args[1:])
	case "stop":
		return evalStopCmd(args[1:])
	}
	return errors.New(evalUsage)
}

const evalUsage = `usage:
  cartograph eval build <dir> -commit <sha> -store <path>    record the flake built at a commit as the build under test
  cartograph eval serve <dir> -document <file> [-agent name] serve a fresh traced run; print the agent's prompt
  cartograph eval call [-as name] <endpoint> --init|--tools|<tool> [<json>|@<file>]
                                                             call the server as an agent does
  cartograph eval score <dir> <run> -criteria <file> -change-set <id>
                                                             score a run from the server and its trace
  cartograph eval status <dir> -criteria <file>              every run, and the streak against the bar
  cartograph eval stop <dir> [<run>]                         stop a run's server, or every run's`

// evalBuildCmd records the build under test: the flake's cartograph
// package built at one commit (just eval-build does the nix build), so
// every counted run serves the same store path whatever changes beside it.
func evalBuildCmd(args []string) error {
	fs := flag.NewFlagSet("eval build", flag.ContinueOnError)
	commit := fs.String("commit", "", "the commit the build is from")
	store := fs.String("store", "", "the Nix store path of the flake's cartograph package built at that commit")
	dir, err := parseWithDir(fs, args)
	if err != nil {
		return err
	}
	if *commit == "" || !strings.HasPrefix(*store, "/nix/store/") {
		return errors.New("give -commit and -store, the store path nix build gave for that commit (just eval-build does both)")
	}
	b := evalBuild{Commit: *commit, Store: *store, Built: time.Now().UTC()}
	if _, err := os.Stat(b.binary()); err != nil {
		return fmt.Errorf("no cartograph in %s: %w", *store, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(dir, "build.json"), b); err != nil {
		return err
	}
	fmt.Printf("build %s frozen at %s\n", short(*commit), b.binary())
	return nil
}

func evalServeCmd(args []string) error {
	fs := flag.NewFlagSet("eval serve", flag.ContinueOnError)
	document := fs.String("document", "", "the document the agent ports")
	agent := fs.String("agent", "Agent", "the name the agent calls the server by, which its trace is read for")
	dir, err := parseWithDir(fs, args)
	if err != nil {
		return err
	}
	if *document == "" {
		return errors.New("give -document, the document the agent ports")
	}
	var b evalBuild
	if err := readJSON(filepath.Join(dir, "build.json"), &b); err != nil {
		return fmt.Errorf("no frozen build in %s: run cartograph eval build first: %w", dir, err)
	}
	runs, err := evalRuns(dir)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%03d", len(runs)+1)
	rd := filepath.Join(dir, "runs", name)
	for _, sub := range []string{"vault", "work"} {
		if err := os.MkdirAll(filepath.Join(rd, sub), 0o755); err != nil {
			return err
		}
	}
	doc := filepath.Join(rd, filepath.Base(*document))
	if err := copyFile(*document, doc, 0o644); err != nil {
		return err
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
	bin := b.binary()
	cmd := exec.Command(bin, "serve", filepath.Join(rd, "vault"), "-addr", addr)
	cmd.Env = append(os.Environ(), "CARTOGRAPH_MCP=on", "CARTOGRAPH_MCP_TRACE="+filepath.Join(rd, "trace.jsonl"))
	cmd.Stdout, cmd.Stderr = logf, logf
	// The server outlives this command: it is its own session.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	endpoint := "http://" + addr + "/api/v1/mcp"
	if err := waitUp("http://"+addr+"/", 30*time.Second); err != nil {
		_ = cmd.Process.Kill()
		return err
	}
	run := evalRun{Name: name, Commit: b.Commit, Endpoint: endpoint, PID: cmd.Process.Pid, Agent: *agent, Document: doc, Started: time.Now().UTC()}
	if err := writeJSON(filepath.Join(rd, "run.json"), run); err != nil {
		return err
	}
	fmt.Printf("run %s on build %s, serving %s\n\nThe agent's prompt:\n\n", name, short(b.Commit), endpoint)
	fmt.Printf("Port the document in %s into Cartograph, and propose the change set. Your person is not available; work from the document alone.\n\n", doc)
	fmt.Printf("Cartograph's MCP server is reached with `%s eval call -as %s %s` (`--init` for its instructions, `--tools`, or `<tool> '<json>'`; `@file.json` for long arguments).\n\n", bin, *agent, endpoint)
	fmt.Printf("Keep scratch files under %s and touch nothing else. Report the change set id.\n", filepath.Join(rd, "work"))
	return nil
}

func evalCallCmd(args []string) error {
	fs := flag.NewFlagSet("eval call", flag.ContinueOnError)
	as := fs.String("as", "Agent", "the name to call the server by")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 2 {
		return errors.New(evalUsage)
	}
	ctx := context.Background()
	c, err := evaluate.Dial(ctx, fs.Arg(0), *as)
	if err != nil {
		return err
	}
	defer c.Close()
	switch fs.Arg(1) {
	case "--init":
		fmt.Println(c.Instructions())
		return nil
	case "--tools":
		tools, err := c.Tools(ctx)
		if err != nil {
			return err
		}
		for _, t := range tools {
			fmt.Printf("- %s: %s\n", t[0], t[1])
		}
		return nil
	}
	callArgs := map[string]any{}
	if fs.NArg() > 2 {
		raw := []byte(fs.Arg(2))
		if path, ok := strings.CutPrefix(fs.Arg(2), "@"); ok {
			if raw, err = os.ReadFile(path); err != nil {
				return err
			}
		}
		if err := json.Unmarshal(raw, &callArgs); err != nil {
			return fmt.Errorf("arguments: %w", err)
		}
	}
	text, err := c.Call(ctx, fs.Arg(1), callArgs)
	if err != nil {
		fmt.Println("ERROR")
		fmt.Println(err)
		return nil
	}
	fmt.Println("OK")
	fmt.Println(text)
	return nil
}

func evalScoreCmd(args []string) error {
	fs := flag.NewFlagSet("eval score", flag.ContinueOnError)
	criteriaPath := fs.String("criteria", "", "the criteria file")
	changeSet := fs.String("change-set", "", "the change set the agent proposed")
	if err := fs.Parse(reorder(args)); err != nil {
		return err
	}
	if fs.NArg() < 2 || *criteriaPath == "" || *changeSet == "" {
		return errors.New(evalUsage)
	}
	dir, name := fs.Arg(0), fs.Arg(1)
	c, err := evaluate.ReadCriteria(*criteriaPath)
	if err != nil {
		return err
	}
	rd := filepath.Join(dir, "runs", name)
	var run evalRun
	if err := readJSON(filepath.Join(rd, "run.json"), &run); err != nil {
		return err
	}
	if c.Agent == "" {
		c.Agent = run.Agent
	}
	doc, err := os.ReadFile(run.Document)
	if err != nil {
		return err
	}
	var calls []trace.Call
	if f, err := os.Open(filepath.Join(rd, "trace.jsonl")); err == nil {
		calls, err = jsonl.Read(f)
		_ = f.Close()
		if err != nil {
			return err
		}
	}
	ctx := context.Background()
	srv, err := evaluate.Dial(ctx, run.Endpoint, "Scorer")
	if err != nil {
		return err
	}
	defer srv.Close()
	s, err := evaluate.ScoreRun(ctx, srv, c, *changeSet, calls, string(doc))
	if err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(rd, "score.json"), s); err != nil {
		return err
	}
	printScore(os.Stdout, name, s)
	return nil
}

func evalStatusCmd(args []string) error {
	fs := flag.NewFlagSet("eval status", flag.ContinueOnError)
	criteriaPath := fs.String("criteria", "", "the criteria file")
	dir, err := parseWithDir(fs, args)
	if err != nil {
		return err
	}
	bar := 3
	name := ""
	if *criteriaPath != "" {
		c, err := evaluate.ReadCriteria(*criteriaPath)
		if err != nil {
			return err
		}
		bar, name = c.Runs, c.Name
	}
	runs, err := evalRuns(dir)
	if err != nil {
		return err
	}
	var scored []evaluate.Run
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "run\tbuild\tchecks\tcalls\tsigma\tfirst pass\tvalue added")
	for _, r := range runs {
		er := evaluate.Run{Name: r.Name, Commit: r.Commit}
		var s evaluate.Score
		if err := readJSON(filepath.Join(dir, "runs", r.Name, "score.json"), &s); err == nil {
			er.Score = &s
			fmt.Fprintf(w, "%s\t%s\t%d/%d\t%d\t%.2f\t%.1f%%\t%.1f%%\n", r.Name, short(r.Commit), s.Passed(), len(s.Checks),
				s.Trace.Calls, s.Trace.Sigma, s.Trace.FirstPassYield*100, s.Trace.ValueAddedRatio*100)
		} else {
			fmt.Fprintf(w, "%s\t%s\tnot scored\t\t\t\t\n", r.Name, short(r.Commit))
		}
		scored = append(scored, er)
	}
	_ = w.Flush()
	st := evaluate.StreakOf(scored, bar)
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

func evalStopCmd(args []string) error {
	if len(args) == 0 {
		return errors.New(evalUsage)
	}
	runs, err := evalRuns(args[0])
	if err != nil {
		return err
	}
	for _, r := range runs {
		if len(args) > 1 && r.Name != args[1] {
			continue
		}
		if p, err := os.FindProcess(r.PID); err == nil && p.Signal(syscall.SIGTERM) == nil {
			fmt.Printf("stopped run %s (pid %d)\n", r.Name, r.PID)
		}
	}
	return nil
}

func printScore(w io.Writer, name string, s evaluate.Score) {
	for _, c := range s.Checks {
		mark := "FAIL"
		if c.Pass {
			mark = "PASS"
		}
		fmt.Fprintf(w, "%s  %s: %s\n", mark, c.Name, c.Why)
	}
	fmt.Fprintf(w, "run %s: %d/%d\n\n", name, s.Passed(), len(s.Checks))
	printReport(w, s.Trace)
}

func evalRuns(dir string) ([]evalRun, error) {
	entries, err := os.ReadDir(filepath.Join(dir, "runs"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []evalRun
	for _, e := range entries {
		var r evalRun
		if e.IsDir() && readJSON(filepath.Join(dir, "runs", e.Name(), "run.json"), &r) == nil {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// parseWithDir parses flags given before or after the directory, the one
// argument these commands take.
func parseWithDir(fs *flag.FlagSet, args []string) (string, error) {
	if err := fs.Parse(reorder(args)); err != nil {
		return "", err
	}
	if fs.NArg() != 1 {
		return "", errors.New(evalUsage)
	}
	return fs.Arg(0), nil
}

// reorder puts flags before positional arguments, so either order parses.
func reorder(args []string) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			flags = append(flags, args[i])
			if !strings.Contains(args[i], "=") && i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
			continue
		}
		rest = append(rest, args[i])
	}
	return append(flags, rest...)
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func waitUp(url string, within time.Duration) error {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if res, err := http.Get(url); err == nil {
			_ = res.Body.Close()
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("the server at %s did not answer within %s", url, within)
}

func copyFile(from, to string, mode os.FileMode) error {
	b, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, b, mode)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func short(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	if commit == "" {
		return "(none)"
	}
	return commit
}
