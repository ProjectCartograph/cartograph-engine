// Package activity is the port through which what people do in an
// interface is recorded (docs/adr/0034), so their work can be judged the
// way agents' calls are (docs/EVALUATING_PEOPLE.md): where a task goes
// wrong, where it waits, where it takes more than it needs. An act is
// recorded by its shape (the kind, the record, the step, the field's
// path, the action, its outcome and how long it took), never by what was
// written: no text a person typed, no name and no figure reaches a trace.
// An adapter keeps the acts; "off", the default, keeps none.
package activity

import (
	"context"
	"fmt"
	"regexp"
	"time"
)

// Sources of an act.
const (
	Server    = "server"    // the engine decided it, for any interface
	Interface = "interface" // only the interface saw it
)

// Outcomes of an act.
const (
	OK      = "ok"
	Refused = "refused" // the engine said no and why
	Failed  = "failed"  // the request could not be answered
)

// Names of the acts the server records on a person's behalf.
const (
	DraftSave       = "draft.save"       // a draft kept, in a change set or the working copy
	DraftDiscard    = "draft.discard"    // a draft thrown away
	VersionSave     = "version.save"     // Save as version, ok or refused
	CheckLeave      = "check.leave"      // a check left open, with a reason
	ChangeSetStart  = "changeset.start"  // a change set opened
	ChangeSetSubmit = "changeset.submit" // a change set sent for review
	ChangeSetAccept = "changeset.accept" // a change set rolled in, ok or refused
	ChangeSetClose  = "changeset.close"  // a change set closed without rolling in
	ChangeSetReopen = "changeset.reopen" // a closed change set opened again
)

// Names of the acts only an interface sees.
const (
	ScreenShow      = "screen.show"      // a screen drawn; Millis until its content, Sign whether a wait showed
	ScreenDeadEnd   = "screen.deadend"   // a screen with nothing to do: an empty state or an error with no action
	FlowOpen        = "flow.open"        // a flow opened on a record; Target new or existing
	StepEnter       = "step.enter"       // a step of a flow entered
	StepBack        = "step.back"        // a step left backwards
	FieldSet        = "field.set"        // a field's answer given (on change, never per key)
	GuideOpen       = "guide.open"       // a field's guide or examples opened
	PickerOpen      = "picker.open"      // a menu, popover or picker opened
	PickerClose     = "picker.close"     // closed; Target chosen or none
	Press           = "press"            // a press; Target action or none; Millis until the next frame
	Request         = "request"          // a request to the server; Millis, Outcome, Sign
	FindOpen        = "find.open"        // the search opened
	FindPick        = "find.pick"        // a record picked from it
	FindClose       = "find.close"       // closed with nothing picked
	ChangeSetSwitch = "changeset.switch" // another change set made the one worked in
)

// interfaceNames are the names an interface may send; anything else is
// refused, so a value cannot travel in a name.
var interfaceNames = map[string]bool{
	ScreenShow: true, ScreenDeadEnd: true, FlowOpen: true, StepEnter: true, StepBack: true,
	FieldSet: true, GuideOpen: true, PickerOpen: true, PickerClose: true, Press: true,
	Request: true, FindOpen: true, FindPick: true, FindClose: true, ChangeSetSwitch: true,
}

// Event is one act. Its names follow OpenTelemetry's general attributes
// where there is one (event.name, session.id, service.version);
// Cartograph's own are under cartograph.*.
type Event struct {
	At     time.Time `json:"time"`
	Name   string    `json:"event.name"`
	Source string    `json:"cartograph.source"`
	// Session groups one person's acts in one window of an interface;
	// server acts have none and are joined to them by person and record.
	Session string `json:"session.id,omitempty"`
	// Build is the server's version, so readings can be taken per build.
	Build     string `json:"service.version,omitempty"`
	Person    string `json:"cartograph.person,omitempty"`
	Interface string `json:"cartograph.interface,omitempty"`
	// Surface is the screen, as its route pattern, never its address
	// with a record's name in it: goals/$id, not goals/feed-the-town.
	Surface   string `json:"cartograph.surface,omitempty"`
	Kind      string `json:"cartograph.kind,omitempty"`
	Record    string `json:"cartograph.record,omitempty"`
	Step      string `json:"cartograph.step,omitempty"`
	Field     string `json:"cartograph.field,omitempty"`
	ChangeSet string `json:"cartograph.change_set,omitempty"`
	// Target is what an act was on, from a short list: new, existing,
	// action, none, chosen.
	Target  string `json:"cartograph.target,omitempty"`
	Outcome string `json:"cartograph.outcome,omitempty"`
	Millis  int64  `json:"cartograph.duration.ms,omitempty"`
	// Sign is whether a wait showed it was waiting: a skeleton or a count.
	Sign bool `json:"cartograph.sign,omitempty"`
	// Problems counts what a refusal said is wrong; Paths are the fields
	// it named, list items as "-".
	Problems int      `json:"cartograph.problems,omitempty"`
	Paths    []string `json:"cartograph.problem.paths,omitempty"`
	// Checks are the ids of the checks still open on a version as it was
	// saved: what was left unmet, never what was written.
	Checks []string `json:"cartograph.checks.open,omitempty"`
}

// Recorder keeps acts. Record never fails the act it records.
type Recorder interface {
	Record(e Event)
}

// Query says which acts to read: those from From (inclusive) to To
// (exclusive), either open when zero, and one person's when Person is set.
type Query struct {
	From, To time.Time
	Person   string
}

// Matches says whether e is one of the acts q asks for.
func (q Query) Matches(e Event) bool {
	return (q.From.IsZero() || !e.At.Before(q.From)) &&
		(q.To.IsZero() || e.At.Before(q.To)) &&
		(q.Person == "" || e.Person == q.Person)
}

// Reader reads acts back for analysis, in the order they happened. It
// is the port every place a trace is kept answers, so the analysis never
// knows where that is.
type Reader interface {
	Read(ctx context.Context, q Query) ([]Event, error)
}

// Off records nothing.
type Off struct{}

// Record does nothing.
func (Off) Record(Event) {}

// IsOff says whether r keeps nothing, so a caller can skip building acts.
func IsOff(r Recorder) bool {
	if r == nil {
		return true
	}
	_, off := r.(Off)
	return off
}

// Stamped stamps every act with the server's build before keeping it.
func Stamped(r Recorder, build string) Recorder { return stamped{r, build} }

type stamped struct {
	Recorder
	build string
}

func (s stamped) Record(e Event) {
	e.Build = s.build
	s.Recorder.Record(e)
}

var (
	token   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,79}$`)
	surface = regexp.MustCompile(`^[A-Za-z0-9_$./-]{0,120}$`)
	pointer = regexp.MustCompile(`^(/([A-Za-z0-9_.-]+|\{[A-Za-z0-9_.:-]+\}|-))*$`)
	targets = map[string]bool{"": true, "new": true, "existing": true, "action": true, "none": true, "chosen": true}
	outcome = map[string]bool{"": true, OK: true, Refused: true, Failed: true}
)

// NewInterfaceAct is the one way an interface's act enters the trace: it
// comes from the interface, and every field holds a shape, or it is
// refused with the reason. An act that could carry what a person wrote
// cannot be made.
func NewInterfaceAct(e Event) (Event, error) {
	e.Source = Interface
	if err := checkInterface(e); err != nil {
		return Event{}, err
	}
	return e, nil
}

// CheckBatch says why a batch's session or interface is refused, or nil:
// both are tokens, never a name.
func CheckBatch(session, iface string) error {
	switch {
	case !token.MatchString(session):
		return fmt.Errorf("session.id is not a token")
	case iface != "" && !token.MatchString(iface):
		return fmt.Errorf("cartograph.interface is not a token")
	}
	return nil
}

// checkInterface says why an interface's act is refused, or nil: an
// unknown name, or a field that could carry something a person wrote.
// Every field is a token from a short alphabet, so no sentence, name or
// figure fits; a field's path is a JSON pointer whose list items are
// keys, never values.
func checkInterface(e Event) error {
	switch {
	case !interfaceNames[e.Name]:
		return fmt.Errorf("event.name %q is not an act an interface records", e.Name)
	case e.Session != "" && !token.MatchString(e.Session):
		return fmt.Errorf("session.id is not a token")
	case e.Interface != "" && !token.MatchString(e.Interface):
		return fmt.Errorf("cartograph.interface is not a token")
	case !surface.MatchString(e.Surface):
		return fmt.Errorf("cartograph.surface is not a route pattern")
	case e.Kind != "" && !token.MatchString(e.Kind):
		return fmt.Errorf("cartograph.kind is not a token")
	case e.Record != "" && !token.MatchString(e.Record):
		return fmt.Errorf("cartograph.record is not an id")
	case e.Step != "" && !token.MatchString(e.Step):
		return fmt.Errorf("cartograph.step is not a token")
	case e.ChangeSet != "" && !token.MatchString(e.ChangeSet):
		return fmt.Errorf("cartograph.change_set is not an id")
	case len(e.Field) > 200 || !pointer.MatchString(e.Field):
		return fmt.Errorf("cartograph.field is not a JSON pointer")
	case !targets[e.Target]:
		return fmt.Errorf("cartograph.target %q is not one of new, existing, action, none, chosen", e.Target)
	case !outcome[e.Outcome]:
		return fmt.Errorf("cartograph.outcome %q is not one of ok, refused, failed", e.Outcome)
	case e.Millis < 0 || e.Millis > int64(time.Hour/time.Millisecond):
		return fmt.Errorf("cartograph.duration.ms is out of range")
	}
	return nil
}
