package sarif

// The wire shape of a SARIF 2.1.0 document, spelled out rather than pulled in
// as a dependency: tarp emits one rule with one kind of location, which is a
// vanishing fraction of the specification, and the fields below are the
// contract other tools parse. Keeping them here means the shape cannot drift
// when the Go structs are rearranged for field alignment.
//
// Everything is `omitempty` where the specification makes it optional, so the
// document stays the smallest thing that is still valid.

// document is the top-level SARIF log.
type document struct {
	Schema  string `json:"$schema"`
	Version string `json:"version"`
	Runs    []run  `json:"runs"`
}

// run is one invocation of one tool over one set of files.
type run struct {
	AutomationDetails  *automationDetails          `json:"automationDetails,omitempty"`
	OriginalURIBaseIDs map[string]artifactLocation `json:"originalUriBaseIds,omitempty"`
	Properties         *runProperties              `json:"properties,omitempty"`
	Results            []result                    `json:"results"`
	Invocations        []invocation                `json:"invocations,omitempty"`
	Tool               tool                        `json:"tool"`
}

// automationDetails distinguishes logically different runs of the same tool.
type automationDetails struct {
	ID string `json:"id"`
}

// runProperties is the property bag carrying what SARIF has no field for.
type runProperties struct {
	Strictness string `json:"strictness"`
	Score      int    `json:"score"`
	Declared   int    `json:"declared"`
	Tested     int    `json:"tested"`
}

// tool names what produced the run.
type tool struct {
	Driver driver `json:"driver"`
}

// driver is the analyzer itself.
type driver struct {
	Name           string `json:"name"`
	Version        string `json:"version,omitempty"`
	InformationURI string `json:"informationUri,omitempty"`
	Rules          []rule `json:"rules"`
}

// rule describes a kind of finding.
type rule struct {
	Properties           *ruleProperties `json:"properties,omitempty"`
	ID                   string          `json:"id"`
	Name                 string          `json:"name"`
	ShortDescription     message         `json:"shortDescription"`
	FullDescription      message         `json:"fullDescription"`
	Help                 message         `json:"help"`
	DefaultConfiguration configuration   `json:"defaultConfiguration"`
}

// ruleProperties carries the tags a consumer groups rules by.
type ruleProperties struct {
	Tags []string `json:"tags,omitempty"`
}

// configuration is a rule's default severity.
type configuration struct {
	Level string `json:"level"`
}

// result is one finding.
type result struct {
	PartialFingerprints map[string]string `json:"partialFingerprints,omitempty"`
	RuleID              string            `json:"ruleId"`
	Level               string            `json:"level"`
	Message             message           `json:"message"`
	Locations           []location        `json:"locations"`
	RuleIndex           int               `json:"ruleIndex"`
}

// location is where a finding is.
type location struct {
	PhysicalLocation physicalLocation `json:"physicalLocation"`
}

// physicalLocation is a file and a range inside it.
type physicalLocation struct {
	ArtifactLocation artifactLocation `json:"artifactLocation"`
	Region           region           `json:"region"`
}

// artifactLocation names a file, optionally relative to a declared base.
type artifactLocation struct {
	URI       string `json:"uri"`
	URIBaseID string `json:"uriBaseId,omitempty"`
}

// region is the span a finding covers.
type region struct {
	StartLine int `json:"startLine"`
	EndLine   int `json:"endLine,omitempty"`
}

// invocation records how the run went.
type invocation struct {
	ToolExecutionNotifications []notification `json:"toolExecutionNotifications,omitempty"`
	ExecutionSuccessful        bool           `json:"executionSuccessful"`
}

// notification is something the tool wants to say about itself rather than
// about the source.
type notification struct {
	Level   string  `json:"level"`
	Message message `json:"message"`
}

// message is SARIF's text-with-optional-markdown shape.
type message struct {
	Text     string `json:"text"`
	Markdown string `json:"markdown,omitempty"`
}
