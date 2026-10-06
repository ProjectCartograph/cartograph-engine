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
// a question an agent porting a real charter had to judge.
var porting = []portingRule{
	// Whose record, and its purpose.
	{"The organisation", "Purpose.organisation (id default)", "The organisation whose work the document is: usually the body that issues it. An umbrella it reports to (a cluster, a parent department) is not it: record that as a governance body if it decides on the work, else name it in the purpose's source."},
	{"Vision and mission", "Purpose.vision, Purpose.mission", "From the organisation's own documents. When none gives them, leave purpose-vision and purpose-mission open for your person with leave_open; never write your own."},
	// The aim tree.
	{"Long-term impact", "Goal, level goal", "The lasting change the organisation pursues."},
	{"Intended outcome of a programme or project", "Goal, level objective, under the goal it serves", "One objective; the work's own outcomes sit under it."},
	{"A logic model's outcomes column", "Goal, level outcome, under the objective", "Each a state that is true once it is reached, not an activity."},
	{"A project's own objectives", "Project.objectives", "The change it makes, then how, in one sentence; keep the document's wording in the source when you rewrite an activity as a change."},
	{"A key performance indicator register", "KPI, each with its owner and cycle; the project names it under kpis", "A measure that is already a KPI is named in the project's kpis with a reason, never restated as a key result. A key result is a measure only this project reaches for."},
	{"A measure taken once", "A success criterion read at closing, or a deliverable's acceptance", "Not a KPI: an indicator is read on a cycle."},
	{"Past readings of a KPI", "KPIReadings (id <kpi id>-readings) drafted in the change set", "One reading per period, filed by the month its period ends; a year alone is that year's last month."},
	{"A gap, shortfall or problem with evidence", "Gap", "statement: the result that falls short, in plain words; quote: the document's own words, never edited to fit; source: document and section."},
	{"Where the evidence was found (districts, regions, kinds of site)", "Segment, the dimension first then its values", "Only the values the document names; a dimension with no named values is recorded as the dimension."},
	// Work.
	{"Workstreams under one sponsor and one budget", "Project components (partOf the project)", "A workstream with its own lead is still a component when it shares the project's sponsor and budget; its lead is the component's manager role, its partners its resources. One with its own sponsor or budget is a project of its own."},
	{"Milestones that name work", "Tasks under the deliverable each produces", "Without their dates; a milestone that is only a date is the planning tool's."},
	{"Milestones already completed", "Not ported", "History, not definition; one that authorised the work (a decision, a directive) is a mandate."},
	{"Success criteria that only say an output was delivered", "That deliverable's acceptance", "A success criterion is the state the outputs are for."},
	{"Success criteria and their dimension", "Project.successCriteria with metric", "efficiency: on time, on budget, to scope. customer: the beneficiaries use it, gain from it or are satisfied. team: the team's capacity or learning. business: the organisation's own results. future: readiness for what comes next (evidence gathered, a baseline set, capacity built). compliance: a requirement met or not."},
	{"A benefits realisation plan", "Success criteria judged after closing (when postClosingCycle)", "Owned by the service owner's role and read on a reporting cycle."},
	{"Legal, safeguarding, consent or data protection requirements", "Success criteria of metric compliance", "Each confirmed by the role that answers for it; personal data also on the data step."},
	{"Deferred or future scope", "Project.summary.scopeOut, as \"Deferred: ...\"", "And a gap where the document gives evidence for it."},
	// Who.
	{"Units of the organisation that carry work", "Team", "A team runs projects and keeps records (data sources)."},
	{"Units the work draws on without carrying it", "Resource, category orgUnit", ""},
	{"Bodies outside the organisation (other ministries, agencies, partners, companies)", "Resource, category externalParty", "Even a sister body inside the same government."},
	{"Committees and boards that decide", "Resource, category governanceBody", "Referenced as a mandate's issuer, who confirms success, whom a risk escalates to, and on the escalation route; never typed as text."},
	{"An escalation route", "Project.escalationRoute (or Programme's)", "The bodies a matter goes up through, nearest first. A body that only receives reports or notes changes is not on it."},
	{"Owners named as units", "A role or unit Resource", "Where a field asks who (an owner, a verifier, who confirms), name the role that answers; a unit that answers as a whole as its orgUnit Resource; several units: the one that leads, the others in the note. Never a team, never a person."},
	{"Names and acronyms", "As the document writes them", "Never expand an acronym from outside knowledge; the person can."},
	// Risk.
	{"Risks", "Risks of type risk, with impact, likelihood, mitigation and owner", ""},
	{"Issues already happening", "Risks of type issue", ""},
	{"Critical dependencies", "Risks of type dependency, with depends naming what is waited on", "Something someone must deliver to the work."},
	{"Constraints", "Risks of type constraint", "A fixed limit the work lives within; no mitigation."},
	{"Key assumptions of the results logic", "Assumption, named by the success criteria that rest on them", "A belief the chain from outputs to outcomes rests on; a thing someone must deliver is a dependency instead, never both."},
	{"Escalation with no target stated", "escalate.to: the next body on the escalation route above the work", "With no route, the sponsor."},
	{"Escalation only if something slips", "escalate.flag false, the trigger in the mitigation", "Flag only what is escalated now."},
	{"Uncosted or unfunded needs", "Risks of type issue, escalated to whoever decides funding", "A funding line once costed."},
	// Money and data.
	{"A service's recurrent costs", "Operation.funding, per month or year", "Never on the project that sets it up; uncosted, an issue on the project's risks."},
	{"A budget broken down by cost line", "One funding line per currency; the breakdown in the resources step's note", "Funding states the money and its source, not its use."},
	{"Data sources kept outside the organisation", "DataSource.keptBy", "team stays your own team that reads it."},
	{"How a data source is captured or classified", "DataSource.capture, .classification", "Only as the document states; left unset otherwise."},
	{"Reporting termly or per survey wave", "A reporting cycle of named periods", "Each by the month it ends; a frequency with no start month: the month the work starts, said in the note."},
	{"Stakeholders", "StakeholderMap entries", "One entry per party: a beneficiary group as group, anyone else as a resource; a group and the body that represents it are two entries. tier primary when directly affected, whatever the document calls it. Influence and interest only as the document scores them; their stake and relationship owner from its engagement plan."},
	// Out.
	{"Decision log, action log, change log, RACI matrix", "Not ported", "Running the work, not defining it; a decision that authorises the work is a mandate."},
	{"Reporting plan, status ratings, readiness assessment, sign-off table", "Not ported", "Planning and document control; reports and their audiences are planning."},
	{"Procurement plan, staffing quantities, time commitments", "Not ported", "Planning; a role the work needs is a resource."},
	{"Evaluation questions", "Not ported", "The document's; the measures that answer them are KPIs or success criteria."},
}
