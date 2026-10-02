package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/auth"
	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store"
)

// accessFile is the mapping from directory groups to roles and teams
// (CARTOGRAPH_ACCESS_FILE, docs/adr/0011), in YAML or JSON:
//
//	roles:
//	  reader: [all-staff]
//	  contributor: [curriculum-division, early-grades-team]
//	  strategyEditor: [planning-unit]
//	  administrator: [cartograph-administrators]
//	teams:
//	  - group: curriculum-division
//	    name: Curriculum division
//	  - group: early-grades-team
//	    name: Early grades
//	    parent: Curriculum division
//
// A team's name is the group's when it has none; its parent is another
// team's name, listed above it or already in Cartograph.
type accessFile struct {
	Roles map[string][]string `yaml:"roles"`
	Teams []struct {
		Group  string `yaml:"group"`
		Name   string `yaml:"name"`
		Parent string `yaml:"parent"`
	} `yaml:"teams"`
	// Agents names the roles whose people may act through an agent
	// (docs/adr/0016).
	Agents []string `yaml:"agents"`
}

// loadDirectory reads an access file. An empty path is an empty mapping:
// no group grants anything.
func loadDirectory(path string) (engine.Directory, error) {
	d := engine.Directory{Roles: map[string][]string{}}
	if path == "" {
		return d, nil
	}
	text, err := os.ReadFile(path)
	if err != nil {
		return d, fmt.Errorf("access file: %w", err)
	}
	var f accessFile
	if err := codecyaml.New().DecodeInto(text, &f); err != nil {
		return d, fmt.Errorf("access file %s: %w", path, err)
	}
	for role, groups := range f.Roles {
		if !slices.Contains(auth.Roles, role) {
			return d, fmt.Errorf("access file %s: %q is not a role (want one of %s)", path, role, strings.Join(auth.Roles, ", "))
		}
		d.Roles[role] = groups
	}
	for _, role := range f.Agents {
		if !slices.Contains(auth.Roles, role) {
			return d, fmt.Errorf("access file %s: agents: %q is not a role (want one of %s)", path, role, strings.Join(auth.Roles, ", "))
		}
	}
	d.Agents = f.Agents
	names := map[string]bool{}
	for i, t := range f.Teams {
		if t.Group == "" {
			return d, fmt.Errorf("access file %s: team %d names no group", path, i+1)
		}
		dt := engine.DirectoryTeam{Group: t.Group, Name: t.Name, Parent: t.Parent}
		name := dt.Name
		if name == "" {
			name = dt.Group
		}
		if names[name] {
			return d, fmt.Errorf("access file %s: two teams are called %q", path, name)
		}
		names[name] = true
		d.Teams = append(d.Teams, dt)
	}
	return d, nil
}

// runAccess is `cartograph access`: create the mapped teams, or list a
// person, from the command line, before anyone has signed in.
func runAccess(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: cartograph access apply|grant <arguments>")
	}
	switch args[0] {
	case "apply":
		return runAccessApply(args[1:])
	case "grant":
		return runAccessGrant(args[1:])
	}
	return fmt.Errorf("cartograph access %s: want apply or grant", args[0])
}

// runAccessApply creates every team the access file maps that does not
// exist yet. Run it once per deployment (a job before the replicas
// start), so two replicas never both create a team.
func runAccessApply(args []string) error {
	positional, flagArgs := splitPositional(args, 1)
	fs := flag.NewFlagSet("access apply", flag.ExitOnError)
	target := fs.String("store", envStore(), "the vault directory, SQLite file or postgres:// URL (CARTOGRAPH_STORE, else CARTOGRAPH_VAULT)")
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if len(positional) != 1 {
		return fmt.Errorf("usage: cartograph access apply <access file> -store <vault, file or postgres:// URL>")
	}
	d, err := loadDirectory(positional[0])
	if err != nil {
		return err
	}
	ctx := context.Background()
	comp, err := compose(ctx, storeOptions{Target: *target, Codec: envCodec(), Fanout: "memory", Access: &d})
	if err != nil {
		return err
	}
	defer comp.Close()
	created, err := comp.Engine.ApplyDirectory(ctx, store.EnrolledByDirectory)
	for _, name := range created {
		fmt.Printf("team created: %s\n", name)
	}
	if err != nil {
		return err
	}
	fmt.Printf("%d teams created; %d mapped\n", len(created), len(d.Teams))
	return nil
}

// runAccessGrant lists a person with roles and teams: how the first
// administrator is named when no directory group grants the role.
func runAccessGrant(args []string) error {
	positional, flagArgs := splitPositional(args, 1)
	fs := flag.NewFlagSet("access grant", flag.ExitOnError)
	target := fs.String("store", envStore(), "the vault directory, SQLite file or postgres:// URL (CARTOGRAPH_STORE, else CARTOGRAPH_VAULT)")
	roles := fs.String("roles", "", "comma-separated: reader, contributor, strategyEditor, administrator")
	teams := fs.String("teams", "", "comma-separated team ids")
	noAgents := fs.Bool("no-agents", false, "turn this person's agents off (docs/adr/0016)")
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if len(positional) != 1 || *roles == "" {
		return fmt.Errorf("usage: cartograph access grant <email> -roles administrator [-teams id,...] -store <...>")
	}
	ctx := context.Background()
	comp, err := compose(ctx, storeOptions{Target: *target, Codec: envCodec(), Fanout: "memory", Access: &engine.Directory{}})
	if err != nil {
		return err
	}
	defer comp.Close()
	p, err := comp.Engine.GrantPerson(ctx, positional[0], splitList(*roles), splitList(*teams), *noAgents, "command line")
	if err != nil {
		return err
	}
	fmt.Printf("%s: %s\n", p.Email, strings.Join(p.Roles, ", "))
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// envStore is where a one-shot command finds the store: CARTOGRAPH_STORE,
// else CARTOGRAPH_VAULT, else the current directory.
func envStore() string {
	if v := os.Getenv("CARTOGRAPH_STORE"); v != "" {
		return v
	}
	if v := os.Getenv("CARTOGRAPH_VAULT"); v != "" {
		return v
	}
	return "."
}
