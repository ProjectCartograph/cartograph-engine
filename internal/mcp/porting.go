package mcp

// portingRule says where one part of a document goes in Cartograph, or
// that it stays out, and how: so an agent porting a plan, a charter or a
// strategy never has to decide what the discipline already has.
type portingRule struct {
	Part string `json:"part"`
	Goes string `json:"goes"`
	How  string `json:"how"`
}

// porting is the map taxonomy returns beside the kinds. Each rule settles
// a question an agent porting a real charter had to judge, and names only
// fields the schemas hold.
var porting = []portingRule{
	// Working without the person.
	{"Your person is not there to ask", "Work from the documents alone", "Port what they state; leave_open each check only your person can answer, with the reason. Never invent a fact to meet a check."},
	{"A check that fails only because of a fact already left open", "leave_open, listing it in also", "One leave_open with also covers the checks the same missing fact fails (a target left open fails smart-measurable and smart-time-bound on the aims it measures)."},

	// Whose record, and its purpose.
	{"The organisation", "Purpose.organisation (id default)", "The organisation whose work the document is: usually the body that issues it. An umbrella it reports to (a cluster, a parent department) is not it: record that as a governance body if it decides on the work, else add it to the purpose's source after the document, as \"<document>; within <umbrella>\"."},
	{"Vision and mission", "Purpose.vision, Purpose.mission", "From the organisation's own documents. When none gives them, leave purpose-vision and purpose-mission open for your person with leave_open, and with also the smart-relevant checks they fail; never write your own. Purpose.source names where they are published, or the document you ported from when they are left open."},
	{"A project or document identifier (\"Project ID\")", "metadata.alias", "As written. The document's version, classification and approval status are document control: not ported."},

	// The aim tree.
	{"Long-term impact", "Goal, level goal", "The lasting change the organisation pursues."},
	{"Intended outcome of a programme or project", "Goal, level objective, under the goal it serves", "One objective; the work's own outcomes sit under it."},
	{"A logic model's outcomes column", "Goal, level outcome, under the objective", "Each a state that is true once it is reached, not an activity."},
	{"A logic model's inputs, activities and outputs", "Inputs: the project's resources and funding. Activities: tasks. Outputs: deliverables", "The outcomes column is the aim tree's (see above)."},
	{"The document's own wording of an aim", "Goal.statedAs, verbatim; Goal.objective in the level's form", "objective is rewritten to the level (an action and its result for a goal, a change for an objective, a state for an outcome) within its length; statedAs keeps the document's sentence as written."},
	{"A goal that is standing policy with no end date", "Goal.horizon ends at its next review", "The year the document says the policy is reviewed (a comprehensive review in a year is that year's end). With no review stated, leave the horizon check open for your person."},
	{"A review that decides whether the policy continues", "The goal's horizon end", "With what it decides in the goal's whyItMatters when it is part of why the aim is held."},
	{"Pilot or proof-of-concept results", "Goal.evidence on the aim they support, and a Gap's source where they show a shortfall", "The finding in a sentence, with where it is written down."},
	{"A project's own objectives", "Project.objectives", "The change it makes, then how, in one sentence. When you rewrite an activity as a change, keep the document's wording in the project's notes under the step key measures."},
	{"A number inside a name (\"Grade 1\", \"Memo 17\")", "Kept in the statement as written", "Only a number that is a target belongs in a key result."},
	{"A project with components", "Key results on the parent's objectives that the components add up to", "The development objective's indicators: what the components achieve together, such as the share of sites where the change is in place. They may repeat an aim's key result when the aim and the project are measured by the same indicator; better still, name that indicator's KPI under kpis."},
	{"A key performance indicator register", "KPI, each with its owner and cycle; the project names it under kpis", "A measure that is already a KPI is named in the project's kpis with a reason, never restated as a key result. A key result is a measure only this project reaches for."},
	{"A measure taken once", "A success criterion read at closing, or a deliverable's acceptance", "Not a KPI: an indicator is read on a cycle. Collecting a baseline is a deliverable."},
	{"A measure taken once, then on a cycle (\"once, then annual\")", "KPI on that cycle", "The first reading is its baseline."},
	{"An impact-level indicator", "KPI with resultLevel impact, goals naming the goal the impact became", "An outcome indicator aligns to its outcome, an output indicator to the objective it serves."},
	{"One indicator read from successive sources", "KPI.source: the source read now", "The definition says which source takes over, and from when; sources is only for a figure computed from several at once. The cycle is the current source's."},
	{"An indicator whose unit the document does not state", "KPI.unit: the word for what is counted or scored (score, points, people)", "Say in the definition that the document states no unit."},
	{"An indicator or target the document defers (\"set after the baseline\")", "The KPI with target left out, kpi-target left open with the reason", "Its aims' smart checks go in the same leave_open's also."},
	{"An outcome with no indicator in the document", "Key results built from the benefits plan or the KPI register rows that name it", "With none, leave smart-measurable and smart-time-bound open for your person."},
	{"Past readings of a KPI", "KPIReadings (id <kpi id>-readings) drafted in the change set", "One reading per period, filed by the month its period ends; a year alone is that year's last month."},
	{"A gap, shortfall or problem with evidence", "Gap", "metadata.name: the result that falls short, in a few words. statement: the document's own words, never edited to fit (up to 400 characters; what does not fit goes in note). current and desired: a short sentence each. source: document and section. measure: the KPI that tracks it, when one exists; without one a sourced gap with both states is a complete observation."},
	{"Where the evidence was found (districts, regions, kinds of site)", "Segment, the dimension first then its values", "Only the values the document names. A dimension with no named values is recorded as the dimension. When the document names values without their dimension (two islands), name the dimension by what varies (Island)."},
	{"Citing a gap that names segments", "The citation's segments: every segment the work reaches", "All of them when the work reaches every one; left empty only for a gap that names none. When you add segments to a gap, name them again on every citation of it."},

	// Work.
	{"Workstreams under one sponsor and one budget", "Project components (partOf the project)", "A workstream with its own lead is still a component when it shares the project's sponsor and budget; its lead is the component's manager role, its partners its resources. One with its own sponsor or budget is a project of its own."},
	{"What a component holds", "Its own objective, problem, scope, deliverables, beneficiaries and manager", "Move each deliverable, and the objective it serves, from the parent to the component that produces it. Its timeline, success criteria, operation, service owner, outcomes, KPIs, funding and mandate are the parent's: leave them out."},
	{"A workstream that is the project's own management or coordination", "The parent project's own work", "Not a component; what it produces stays among the parent's deliverables."},
	{"A workstream the document also puts out of scope, or that another body leads outside the organisation", "Not a component; a scopeOut line", "Name it in the parent's scopeOut, and as a dependency when the work waits on it."},
	{"Milestones that name work", "Tasks under the deliverable each produces", "Without their dates; a milestone that is only a date is the planning tool's."},
	{"Milestones already completed", "Not ported", "History, not definition; one that authorised the work (a decision, a directive) is a mandate."},
	{"Project phases", "Project.timeline: start month and phases in whole months", "Each phase from the month it starts to the month the next starts; when the document's phases overlap, the later one starts where the earlier ends, and the overlap is said in the notes under the step key timeline."},
	{"Handover requirements (what passes to the service)", "Deliverables accepted by the service owner's role, and success criteria when atLanding", "What is handed over is a deliverable; that the service runs with it is a criterion."},
	{"Success criteria that only say an output was delivered", "That deliverable's acceptance", "A success criterion is the state the outputs are for."},
	{"Success criteria and their dimension", "Project.successCriteria with metric", "efficiency: on time, on budget, to scope. customer: the beneficiaries use it, gain from it or are satisfied. team: the team's capacity or learning. business: the organisation's own results. future: readiness for what comes next (evidence gathered, a baseline set, capacity built). compliance: a requirement met or not, and a decision taken or not, confirmed by the role or body that takes it."},
	{"Pointing a success criterion at the deliverables behind it", "successCriteria from: {\"local\":\"deliverables\",\"id\":\"<deliverable id>\"}", "Optional; only where a deliverable produces the outcome."},
	{"A benefits realisation plan", "Success criteria judged after closing (postClosingCycle)", "Owned by the service owner's role and read on a reporting cycle. Its review dates are the cycle; its statuses are progress, not ported."},
	{"Legal, safeguarding, consent, data protection, accessibility, equity or environmental requirements", "Success criteria of metric compliance", "Each confirmed by the role that answers for it; personal data also on the data step."},
	{"Deferred or future scope", "Project.summary.scopeOut, as \"Deferred: ...\"", "And a gap where the document gives evidence for it. Each line up to 60 characters: shorten to the thing, the detail in the notes under the step key scope."},
	{"The people served, and counts of them", "BeneficiaryGroup, named without numbers", "A count that is a measure is a KPI's baseline; a sample size is a data source's quality issue; a headcount the work draws on is planning, not ported."},
	{"A beneficiary that is not a group of people (a public system)", "A StakeholderMap entry as a resource (externalParty or orgUnit)", "Not a BeneficiaryGroup."},

	// Who.
	{"Units of the organisation that carry work", "Team", "A team runs projects and keeps records (data sources)."},
	{"A unit that carries work and is named as an owner", "A Team for the work, and the role that heads it as a Resource", "Owner fields name the role (\"Head, <unit>\"); a unit that answers as a whole is also an orgUnit Resource. Never a team in an owner field."},
	{"Units the work draws on without carrying it", "Resource, category orgUnit", ""},
	{"Bodies outside the organisation (other ministries, agencies, partners, companies)", "Resource, category externalParty", "Even a sister body inside the same government."},
	{"Committees and boards that decide", "Resource, category governanceBody", "Referenced as a mandate's issuer, who confirms success, whom a risk escalates to, and on the escalation route; never typed as text."},
	{"An escalation route", "Project.escalationRoute (or Programme's)", "The bodies a matter goes up through, nearest first. A body that only receives reports or notes changes is not on it."},
	{"Owners named as units", "A role or unit Resource", "Where a field asks who (an owner, a verifier, who confirms), name the role that answers; a unit that answers as a whole as its orgUnit Resource. Several: the one that leads in the field, the others in the KPI's definition or the notes under that step's key. Never a team, never a person."},
	{"Roles and bodies a definition names", "Resource, defined before the goal, project, programme or KPI that names them", "A project's own positions then go in its resources, each pointing at its Resource; every Resource declared should be named somewhere (checks lists those nothing names)."},
	{"Names and acronyms", "As the document writes them", "Never expand an acronym from outside knowledge; the person can."},
	{"A service the document does not name", "Operation, status planned, named by what it does", "Such as \"<policy> compliance monitoring\"; its owner role the one the document gives the running work to."},

	// Risk.
	{"Risks", "Risks of type risk, with impact, likelihood, mitigation and owner", ""},
	{"Issues already happening", "Risks of type issue", ""},
	{"Critical dependencies", "Risks of type dependency, with depends naming what is waited on", "Something someone must deliver to the work. Its mitigation from the document's matching risk row; with none, leave risks-mitigation open for your person."},
	{"Constraints and fixed external dates", "Risks of type constraint", "A fixed limit the work lives within; no mitigation."},
	{"Key assumptions of the results logic", "Assumption, named by the success criteria that rest on them", "A belief the chain from outputs to outcomes rests on; a thing someone must deliver is a dependency instead, never both."},
	{"Schedule and context assumptions (dates hold, funding stays, people stay available)", "Risks of type risk, the assumption failing as the risk", "Not an Assumption: those are the results logic's."},
	{"Escalation with no target stated", "escalate.to: the next body on the escalation route above the work", "With no route, the sponsor."},
	{"Escalation only if something slips", "escalate.flag false, the trigger in the mitigation", "Flag only what is escalated now."},
	{"Uncosted or unfunded needs", "Risks of type issue, escalated to whoever decides funding", "A funding line once costed."},

	// Money and data.
	{"Funding status", "funding status: approved when the funder has approved it; requested when it is noted, pending or asked for; unfunded when none is found", "A condition on it goes in the notes under the step key resources."},
	{"A service's recurrent costs", "Operation.funding, per month or year", "Never on the project that sets it up. Uncosted: an issue on the project's risks, and landing-service-funding left open with that reason."},
	{"A budget broken down by cost line", "One funding line per currency; the breakdown in the notes under the step key resources", "Funding states the money and its source, not its use."},
	{"Data sources kept outside the organisation", "DataSource.keptBy, provenance external", "team stays your own team that reads it. provenance says who runs the source (internal or external); keptBy names that body."},
	{"How a data source is captured, classified or categorised", "DataSource.capture, .classification, .category", "Only as the document states; left unset otherwise."},
	{"Reporting termly or per survey wave", "A reporting cycle of named periods", "Each by the month it ends. A frequency tied to a term (\"quarterly in Term III\") is named periods ending in the months the document gives. A frequency with no start month: the month the work starts, said in the cycle's name or the notes."},
	{"Stakeholders", "StakeholderMap entries", "One entry per party: a beneficiary group as group, anyone else as a resource; a group and the body that represents it are two entries. tier primary when the work changes what they get or must do (the beneficiaries, those who must comply), secondary otherwise, whatever the document calls them. Influence and interest only as the document scores them; their stake and relationship owner from its engagement plan."},

	// Out.
	{"Decision log, action log, change log, RACI matrix", "Not ported", "Running the work, not defining it; a decision that authorises the work is a mandate."},
	{"Reporting plan, status ratings, readiness assessment, sign-off table", "Not ported", "Planning and document control; reports and their audiences are planning."},
	{"Procurement plan, staffing quantities, time commitments", "Not ported", "Planning; a role the work needs is a resource."},
	{"Evaluation questions", "Not ported", "The document's; the measures that answer them are KPIs or success criteria."},
}
