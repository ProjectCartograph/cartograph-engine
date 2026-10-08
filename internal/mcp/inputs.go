package mcp

import "github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"

// The tools' inputs.

// Inputs.
type (
	none     struct{}
	kindOnly struct {
		Kind string `json:"kind" jsonschema:"a kind, such as Goal or Project"`
	}
	registerIn struct {
		ChangeSet string   `json:"changeSet,omitempty" jsonschema:"the change set to work in; your latest open one when left out"`
		Kind      string   `json:"kind" jsonschema:"the record's kind: Project"`
		ID        string   `json:"id"`
		Field     string   `json:"field" jsonschema:"the list to write: /spec/milestones, /spec/deliverables, /spec/risks or /spec/kpis"`
		Section   string   `json:"section" jsonschema:"the section holding the table, by the id the outline gave"`
		Work      []string `json:"work,omitempty" jsonschema:"the work list start_work returned"`
	}
	portIn struct {
		ChangeSet string              `json:"changeSet,omitempty" jsonschema:"the change set to port into; your latest open one when left out, and a new one when you have none"`
		Title     string              `json:"title" jsonschema:"the document's title"`
		Text      string              `json:"text,omitempty" jsonschema:"the document's whole text, straight from its file: on the first call only"`
		FileSize  int                 `json:"fileSize,omitempty" jsonschema:"with text: the file's size, a number of bytes (wc -c, os.path.getsize): a text that is not the whole file is refused"`
		Pieces    []any               `json:"pieces,omitempty" jsonschema:"on the second call: every piece of work the document names, each with its name and only its yes answers to the structure questions"`
		Records   []engine.PortRecord `json:"records,omitempty" jsonschema:"on the third call: every record the second call listed, each with set (every field the document gives, by JSON pointer) and open (each check it does not answer, with the reason)"`
	}
	bringIn struct {
		ChangeSet string `json:"changeSet,omitempty" jsonschema:"the change set to keep it in; your latest open one when left out, and a new one when you have none"`
		Title     string `json:"title" jsonschema:"the document's title"`
		Text      string `json:"text" jsonschema:"the document's whole text, straight from its file"`
		FileSize  int    `json:"fileSize,omitempty" jsonschema:"the file's size, a number of bytes (os.path.getsize): a text that is not the whole file is refused"`
	}
	readSectionIn struct {
		ChangeSet string   `json:"changeSet,omitempty" jsonschema:"the change set the document is in; your latest open one when left out"`
		IDs       []string `json:"ids" jsonschema:"the sections to read, by the ids the outline gave"`
	}
	settleIn struct {
		ChangeSet string         `json:"changeSet,omitempty" jsonschema:"the change set to work in; your latest open one when left out"`
		Kind      string         `json:"kind"`
		ID        string         `json:"id"`
		Set       map[string]any `json:"set,omitempty" jsonschema:"every field the documents give, by JSON pointer, such as {\"/spec/summary/about\": \"...\", \"/spec/objectives/0/objective\": \"...\"}"`
		Unset     []string       `json:"unset,omitempty" jsonschema:"fields to clear, by JSON pointer"`
		Open      []settleOpen   `json:"open,omitempty" jsonschema:"each check on this record the documents do not answer, with the reason your person will read"`
		Asked     string         `json:"asked,omitempty" jsonschema:"what you asked your person and what they answered; \"not available\" when you were told to work without them. Required with open"`
		Work      []string       `json:"work,omitempty" jsonschema:"the work list start_work returned: the answer then says what comes next across it"`
	}
	settleOpen struct {
		Check  string `json:"check"`
		Reason string `json:"reason"`
	}
	leaveIn struct {
		ChangeSet string      `json:"changeSet,omitempty" jsonschema:"the change set to work in: your own, or your person's when they ask you to help with it (change_sets lists them); your latest open one when left out"`
		Kind      string      `json:"kind"`
		ID        string      `json:"id"`
		Check     string      `json:"check,omitempty" jsonschema:"the check's id, as checks reports it"`
		Reason    string      `json:"reason" jsonschema:"what your person must supply or decide, in one line they can act on; empty takes it back"`
		Also      []leaveItem `json:"also,omitempty" jsonschema:"more checks the same missing fact leaves open, on this draft or others, each {kind, id, check}: one reason for all of them"`
		Correct   bool        `json:"correct,omitempty" jsonschema:"true to put this reason in place of the one already given; left out, a second fact behind the same check adds its reason to the first"`
		Asked     string      `json:"asked,omitempty" jsonschema:"what you asked your person, with the reason you gave, and what they answered; \"not available\" only when you were told to work without them. Required to leave a check"`
	}
	leaveItem struct {
		Kind  string `json:"kind"`
		ID    string `json:"id"`
		Check string `json:"check"`
	}
	structureIn struct {
		Pieces []any `json:"pieces" jsonschema:"every piece of work the documents or your person name, each an object with its name and its answers to the structure questions"`
	}
	semanticIn struct {
		ChangeSet string `json:"changeSet,omitempty" jsonschema:"the change set to read in; your latest open one when left out"`
		Format    string `json:"format,omitempty" jsonschema:"the syntax, such as dbt; the deployment's first when left out"`
	}
	readingIn struct {
		ChangeSet string `json:"changeSet,omitempty" jsonschema:"the change set to read in: your own, or your person's when they ask you to help with it; your latest open one when left out"`
	}
	getIn struct {
		ChangeSet string   `json:"changeSet,omitempty" jsonschema:"the change set to read in; your latest open one when left out"`
		Kind      string   `json:"kind,omitempty" jsonschema:"one manifest's kind"`
		ID        string   `json:"id,omitempty" jsonschema:"one manifest's id"`
		Records   []string `json:"records,omitempty" jsonschema:"several manifests at once, each as Kind/id: read every record you need in one call"`
	}
	manifestRef struct {
		ChangeSet string `json:"changeSet,omitempty" jsonschema:"the change set to work in: your own, or your person's when they ask you to help with it (change_sets lists them); your latest open one when left out, and a new one when you have none"`
		Kind      string `json:"kind" jsonschema:"the manifest's kind"`
		ID        string `json:"id" jsonschema:"the manifest's id"`
	}
	searchIn struct {
		Kind  string   `json:"kind" jsonschema:"the kind to list"`
		Query string   `json:"query,omitempty" jsonschema:"words in the id or name"`
		Refs  []string `json:"refs,omitempty" jsonschema:"Kind/id that every result must reference"`
		After string   `json:"after,omitempty" jsonschema:"the last id of the previous page"`
		Limit int      `json:"limit,omitempty" jsonschema:"at most this many, 50 when left out"`
	}
	diffIn struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
		From int    `json:"from" jsonschema:"the earlier version"`
		To   int    `json:"to" jsonschema:"the later version"`
	}
	taxonomyIn struct {
		Locale string `json:"locale,omitempty"`
		Part   string `json:"part,omitempty" jsonschema:"a few words of the parts to port now, such as milestone or budget: answers with how to write each part that matches"`
		Full   bool   `json:"full,omitempty" jsonschema:"true for the whole porting map with how to write every part; long"`
	}
	matchIn struct {
		Kind  string `json:"kind" jsonschema:"the kind to look in"`
		Level string `json:"level,omitempty" jsonschema:"for a Goal, the level: goal, objective or outcome"`
		Text  string `json:"text" jsonschema:"what the new thing would say: its name and statement, in the document's words"`
	}
	fromIdeaIn struct {
		Kind string `json:"kind" jsonschema:"the kind being defined, such as Project"`
		Idea string `json:"idea" jsonschema:"the work as your person described it, in their words"`
	}
	relevantIn struct {
		Text  string   `json:"text" jsonschema:"what the work is about, and what has been written of it so far"`
		Kinds []string `json:"kinds,omitempty" jsonschema:"the kinds to rank, such as Goal, KPI, Programme, BeneficiaryGroup, DataSource; every kind a piece of work names when left out"`
		Level string   `json:"level,omitempty" jsonschema:"for Goal, the level to rank among: goal, objective or outcome"`
	}
	startWorkIn struct {
		Title       string `json:"title" jsonschema:"what this piece of work is, as your person would say it"`
		Description string `json:"description,omitempty" jsonschema:"what it is for, and what it will hold"`
		Pieces      []any  `json:"pieces,omitempty" jsonschema:"the pieces of work with their answers, as you gave them to structure: every record they make is written as a first draft"`
	}
	proposeIn struct {
		ChangeSet  string                       `json:"changeSet,omitempty" jsonschema:"the change set to propose; your latest open one when left out"`
		Reason     string                       `json:"reason" jsonschema:"why, in a sentence the person will read"`
		OpenChecks map[string]map[string]string `json:"openChecks,omitempty" jsonschema:"only for checks you cannot meet without your person: Kind/id to (check id to why); every other open check refuses the proposal"`
	}
	nextIn struct {
		ChangeSet string   `json:"changeSet,omitempty" jsonschema:"the change set to work in: your own, or your person's when they ask you to help with it (change_sets lists them); your latest open one when left out, and a new one when you have none"`
		Work      []string `json:"work,omitempty" jsonschema:"every manifest in this piece of work, as Kind/id; leave it out for the stage of the workspace to write now"`
		Locale    string   `json:"locale,omitempty"`
	}
	editIn struct {
		ChangeSet string         `json:"changeSet,omitempty" jsonschema:"the change set to work in: your own, or your person's when they ask you to help with it (change_sets lists them); your latest open one when left out, and a new one when you have none"`
		Kind      string         `json:"kind"`
		ID        string         `json:"id"`
		Work      []string       `json:"work,omitempty" jsonschema:"every other manifest you are defining with this one, as Kind/id (the ones you will propose together): their drafts are read and checked with it, and what they still lack is reported in around"`
		Set       map[string]any `json:"set,omitempty" jsonschema:"fields to set, by JSON pointer, such as {\"/spec/objective\": \"...\"}; in a list, a number replaces that item, and - (or the next number) appends one; null removes the field"`
		Unset     []string       `json:"unset,omitempty" jsonschema:"fields or list items to remove, by JSON pointer"`
	}
	manifestIn struct {
		ChangeSet string         `json:"changeSet,omitempty" jsonschema:"the change set to work in: your own, or your person's when they ask you to help with it (change_sets lists them); your latest open one when left out, and a new one when you have none"`
		Kind      string         `json:"kind"`
		ID        string         `json:"id"`
		Manifest  map[string]any `json:"manifest" jsonschema:"the whole manifest: apiVersion, kind, metadata and spec"`
		Work      []string       `json:"work,omitempty" jsonschema:"every other manifest you are defining with this one, as Kind/id (the ones you will propose together): their drafts are read and checked with it, and what they still lack is reported in around"`
	}
	saveManyIn struct {
		ChangeSet string           `json:"changeSet,omitempty" jsonschema:"the change set to work in; your latest open one when left out, and a new one when you have none"`
		Manifests []map[string]any `json:"manifests" jsonschema:"the whole manifests, each with apiVersion, kind, metadata (with id and name) and spec"`
		Work      []string         `json:"work,omitempty" jsonschema:"the work list structure returned, as Kind/id: the answer then says what to do next across it"`
	}
	saveIn struct {
		ChangeSet string         `json:"changeSet,omitempty" jsonschema:"the change set to work in: your own, or your person's when they ask you to help with it (change_sets lists them); your latest open one when left out, and a new one when you have none"`
		Kind      string         `json:"kind"`
		ID        string         `json:"id"`
		Manifest  map[string]any `json:"manifest" jsonschema:"the whole manifest: apiVersion, kind, metadata and spec"`
		Replace   bool           `json:"replace,omitempty" jsonschema:"true to replace a draft this change set already holds, every field of it; leave out to create, and change an existing draft with edit_draft"`
		Work      []string       `json:"work,omitempty" jsonschema:"every other manifest you are defining with this one, as Kind/id (the ones you will propose together): their drafts are read and checked with it, and what they still lack is reported in around"`
	}
	proposeSaveIn struct {
		ChangeSet string         `json:"changeSet,omitempty" jsonschema:"the change set to work in: your own, or your person's when they ask you to help with it (change_sets lists them); your latest open one when left out, and a new one when you have none"`
		Kind      string         `json:"kind"`
		ID        string         `json:"id"`
		Manifest  map[string]any `json:"manifest,omitempty" jsonschema:"the manifest to save; the current draft when left out"`
		Reason    string         `json:"reason" jsonschema:"why, in a sentence the person will read"`
		// Checks the agent could not meet, each with why; the person sees
		// them on the proposal.
		OpenChecks map[string]string `json:"openChecks,omitempty" jsonschema:"only for a check you cannot meet without your person: check id to why it is left open; every other open check refuses the proposal"`
	}
	proposeSetIn struct {
		ChangeSet string        `json:"changeSet,omitempty" jsonschema:"the change set to work in: your own, or your person's when they ask you to help with it (change_sets lists them); your latest open one when left out, and a new one when you have none"`
		Manifests []setMemberIn `json:"manifests" jsonschema:"every manifest that stands or falls together, in any order; each is saved after those it references"`
		Reason    string        `json:"reason" jsonschema:"why, in a sentence the person will read"`
		// Checks the agent could not meet, by Kind/id, then check id.
		OpenChecks map[string]map[string]string `json:"openChecks,omitempty" jsonschema:"only for checks you cannot meet without your person: Kind/id to (check id to why); every other open check refuses the set"`
	}
	setMemberIn struct {
		Kind     string         `json:"kind"`
		ID       string         `json:"id"`
		Manifest map[string]any `json:"manifest,omitempty" jsonschema:"the manifest; its current draft when left out"`
	}
	checksIn struct {
		ChangeSet string         `json:"changeSet,omitempty" jsonschema:"the change set to work in: your own, or your person's when they ask you to help with it (change_sets lists them); your latest open one when left out, and a new one when you have none"`
		Kind      string         `json:"kind,omitempty" jsonschema:"the manifest's kind; leave kind and id out to check the whole change set, as propose will"`
		ID        string         `json:"id,omitempty"`
		Manifest  map[string]any `json:"manifest,omitempty" jsonschema:"a manifest to check without saving it; its draft or latest version when left out"`
		Work      []string       `json:"work,omitempty" jsonschema:"every other manifest you are defining with this one, as Kind/id (the ones you will propose together): their drafts are read and checked with it, and what they still lack is reported in around"`
	}
	guideIn struct {
		Kind   string `json:"kind"`
		Level  string `json:"level,omitempty" jsonschema:"for a Goal: goal, objective or outcome"`
		Locale string `json:"locale,omitempty" jsonschema:"the language of the words, such as en; English when there are none in it"`
		Step   string `json:"step,omitempty" jsonschema:"one step's key, to read a long guide a step at a time"`
	}
	proposeItemIn struct {
		Kind   string         `json:"kind" jsonschema:"KPIReadings for a reading"`
		ID     string         `json:"id"`
		Series string         `json:"series" jsonschema:"the series under spec, readings for a reading"`
		Item   map[string]any `json:"item" jsonschema:"the item: for a reading, period (YYYY-MM) and value, and provisional and note when they apply"`
		Reason string         `json:"reason"`
	}
	proposeStateIn struct {
		Project string `json:"project" jsonschema:"the project's id"`
		To      string `json:"to" jsonschema:"defined, handed off or cancelled"`
		Reason  string `json:"reason" jsonschema:"why; a cancellation needs one"`
	}
	reportIn struct {
		Name string `json:"name" jsonschema:"projects, kpi-readings, alignment or teams"`
	}
	eventsIn struct {
		After int64 `json:"after,omitempty" jsonschema:"the seq of the last event read; 0 for the start"`
		Limit int   `json:"limit,omitempty"`
	}
)
