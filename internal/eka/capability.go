// Package eka implements the EKA capability layer of eka-mcp: the thin
// adapter between the MCP server and the eka-core Runtime.
//
// It wraps the runtime services (Resolver, Knowledge, Workspace,
// Authoring, Integrity) and the deterministic machine projections
// (machine.NewDocument, machine.NewCollection, contexts.Engine) behind
// the capability surface the MCP server dispatches to. It deliberately
// reimplements NO EKA domain logic — every call delegates to eka-core;
// this package only shapes the wire responses (the deterministic
// eka-*-v1 schema shapes).
package eka

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/maleolabs/eka-core/conformance"
	"github.com/maleolabs/eka-core/contexts"
	"github.com/maleolabs/eka-core/exchange"
	"github.com/maleolabs/eka-core/machine"
	"github.com/maleolabs/eka-core/runtime"
	"github.com/maleolabs/eka-mcp/internal/mcp"
)

// Capability is one opened EKA capability: a handle on the EKA Runtime
// exposing the knowledge-retrieval, context and authoring surfaces.
type Capability struct {
	rt *runtime.Runtime
}

// Open opens the EKA Runtime read-only (runtime.Open — it never
// initializes a workspace; a missing workspace yields a detached
// runtime whose service calls report the uninitialized state
// informatively). The server can therefore initialize cleanly even
// before a workspace exists.
func Open() (*Capability, error) {
	rt, err := runtime.Open()
	if err != nil {
		return nil, fmt.Errorf("eka: cannot open the EKA runtime: %w", err)
	}
	return &Capability{rt: rt}, nil
}

// Close closes the underlying runtime (a no-op on a detached runtime).
func (c *Capability) Close() error {
	return c.rt.Close()
}

// Exists reports whether the EKA workspace is initialized.
func (c *Capability) Exists() bool {
	return c.rt.Exists()
}

// Get resolves one identity form to its current Canonical Knowledge
// Object and returns the machine document (schema eka-cko-v2, compact
// form). Accepted forms: canonical "<ns>/<type>:<id>:<v>" or the
// qualified line form "<ns>/<type>:<id>" (the latest instance of the
// line). Resolution and parsing are entirely eka-core's
// (Resolver.Resolve + conformance.ParseReference).
//
// When the workspace is not initialized, it returns a deterministic
// uninitialized error without leaking store paths — the MCP boundary
// sanitizes any residual path, but the capability already avoids it.
func (c *Capability) Get(form string) ([]byte, error) {
	if !c.Exists() {
		return nil, fmt.Errorf("eka: workspace not initialized")
	}
	u, ok, err := c.rt.Resolver.Resolve(form)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("eka: no object resolves from %q", form)
	}
	doc, err := machine.NewDocument(u)
	if err != nil {
		return nil, err
	}
	return doc.MarshalCompact()
}

// Domain returns every unit of one Engineering Domain of a project as a
// machine collection (schema eka-cko-v2, sorted by canonical form,
// compact form). The search itself is eka-core's (Knowledge.Search);
// NewCollection provides the deterministic domain projection.
func (c *Capability) Domain(projectID, domain string) ([]byte, error) {
	if !c.Exists() {
		return nil, fmt.Errorf("eka: workspace not initialized")
	}
	units, err := c.rt.Knowledge.Search(runtime.SearchQuery{ProjectID: projectID, Domain: domain})
	if err != nil {
		return nil, err
	}
	col, err := machine.NewCollection(domain, units)
	if err != nil {
		return nil, err
	}
	return col.MarshalCompact()
}

// Status returns the aggregated workspace status as JSON (eka-core's
// Workspace.Status — the same aggregation `eka status` serves).
//
// When the workspace is not initialized (detached runtime, EKA_HOME
// unset or empty), it returns a deterministic uninitialized shape
// instead of an error, so the MCP server answers cleanly without a
// workspace — the initialized flag is the deterministic signal.
func (c *Capability) Status() ([]byte, error) {
	if !c.Exists() {
		return json.Marshal(map[string]any{
			"schema":      "eka-status-v1",
			"initialized": false,
			"path":        c.rt.Path(),
			"message":     "workspace not initialized: run 'eka project register' to create it",
			"objects":     0,
			"payloads":    0,
			"attachments": 0,
			"projects":    []any{},
		})
	}
	st, err := c.rt.Workspace.Status()
	if err != nil {
		return nil, err
	}
	return json.Marshal(st)
}

// Context builds the Context Object around one subject at one depth
// (schema eka-context-v1, compact form). The depth token is resolved
// by contexts.ParseDepth (local | dependency | engineering); the
// construction itself is entirely the Context Engine's.
func (c *Capability) Context(subject, projectID, depth string) ([]byte, error) {
	d, ok := contexts.ParseDepth(depth)
	if !ok {
		return nil, fmt.Errorf("eka: unknown context depth %q (allowed: local, dependency, engineering)", depth)
	}
	if !c.Exists() {
		return nil, fmt.Errorf("eka: workspace not initialized")
	}
	obj, err := contexts.New(c.rt).Build(subject, projectID, d, contexts.Options{})
	if err != nil {
		return nil, err
	}
	return obj.MarshalCompact()
}

// Validate runs the authoring conformance gate over the repository
// rooted at root and returns the machine report (schema
// eka-conformance-report-v1). The scan is entirely eka-core's
// (Authoring.Validate); this method only shapes the deterministic
// report projection.
func (c *Capability) Validate(root string) ([]byte, error) {
	report, err := runtime.Authoring.Validate(root)
	if err != nil {
		return nil, err
	}
	results := make([]conformanceResult, 0, len(report.Results))
	for _, r := range report.SortedResults() {
		results = append(results, conformanceResult{
			File:     r.File,
			Rule:     r.Rule,
			Severity: string(r.Severity),
			Message:  r.Message,
		})
	}
	return json.Marshal(conformanceReport{
		Schema:       "eka-conformance-report-v1",
		Root:         report.Root,
		FilesScanned: report.FilesScanned,
		Artifacts:    report.Artifacts,
		Skipped:      report.Skipped,
		Errors:       report.ErrorCount(),
		Warnings:     report.WarningCount(),
		Pass:         report.Pass(),
		Results:      results,
	})
}

// NewDraft scaffolds one draft in the workspace drafts tree (schema
// eka-draft-v1). The inline content object is staged to a temporary
// file and merged by eka-core's NewDraft (the agent content path); the
// change-log authority is the resolved agent identity — never the
// "Engineering" placeholder.
func (c *Capability) NewDraft(req mcp.NewDraftRequest) ([]byte, error) {
	contentFile, err := stageContent(req.Content)
	if err != nil {
		return nil, err
	}
	if contentFile != "" {
		defer os.Remove(contentFile)
	}
	draft, err := runtime.Authoring.NewDraft(c.rt, runtime.NewDraftRequest{
		Project:       req.Project,
		Namespace:     req.Namespace,
		Type:          req.Type,
		ID:            req.ID,
		Dimension:     req.Dimension,
		Phase:         req.Phase,
		Domain:        req.Domain,
		By:            toAuthorIdentity(req.By),
		Relationships: toRelationships(req.Relationships),
		ContentFile:   contentFile,
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(draftResult{
		Schema:    "eka-draft-v1",
		Project:   draft.Project,
		Namespace: draft.Namespace,
		Type:      draft.Type,
		ID:        draft.ID,
		Path:      draft.Path,
		Updated:   draft.Updated,
	})
}

// Publish publishes one draft as an immutable Canonical Knowledge
// Object (schema eka-publish-result-v1). All-or-nothing: a failed
// validation or insert leaves the draft untouched; the draft file is
// the single-use ticket.
func (c *Capability) Publish(req mcp.PublishRequest) ([]byte, error) {
	res, err := runtime.Authoring.Publish(c.rt, req.Target, runtime.PublishOptions{
		Project:         req.Project,
		InstanceVersion: req.InstanceVersion,
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(publishResult{
		Schema:          "eka-publish-result-v1",
		Form:            res.Form,
		InstanceVersion: res.InstanceVersion,
		ObjectHash:      res.ObjectHash,
		Note:            res.Note,
	})
}

// Transition performs one transition (schema eka-transition-result-v1).
// The D1 table, the R13 note gates and the active-container
// confirmation are enforced by eka-core's Authoring.Transition — a
// refused transition publishes nothing.
func (c *Capability) Transition(req mcp.TransitionRequest) ([]byte, error) {
	res, err := runtime.Authoring.Transition(c.rt, runtime.TransitionRequest{
		RepoPath:  req.RepoPath,
		Target:    req.Target,
		To:        req.To,
		Forward:   req.Forward,
		Backward:  req.Backward,
		By:        req.By.Name,
		ByKind:    req.By.Kind,
		Confirmed: req.Confirmed,
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(transitionResult{
		Schema:         "eka-transition-result-v1",
		Target:         res.Target,
		From:           res.From,
		To:             res.To,
		By:             res.By,
		ObjectHash:     res.ObjectHash,
		LockedPlan:     res.LockedPlan,
		LockedPlanHash: res.LockedPlanHash,
		Warning:        res.Warning,
	})
}

// Note creates one cmt- note draft (schema eka-note-result-v1). The
// inline content object is staged to a temporary file and merged over
// the per-role template by eka-core's NoteDraft.
func (c *Capability) Note(req mcp.NoteRequest) ([]byte, error) {
	contentFile, err := stageContent(req.Content)
	if err != nil {
		return nil, err
	}
	if contentFile != "" {
		defer os.Remove(contentFile)
	}
	res, err := runtime.Authoring.NoteDraft(c.rt, runtime.NoteDraftRequest{
		RepoPath:    req.RepoPath,
		Target:      req.Target,
		Role:        req.Role,
		Domain:      req.Domain,
		By:          toAuthorIdentity(req.By),
		ContentFile: contentFile,
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(noteResult{
		Schema:       "eka-note-result-v1",
		ID:           res.ID,
		Target:       res.Target,
		SubjectState: res.SubjectState,
		Path:         res.Path,
		By:           res.By,
	})
}

// DraftRead returns one draft file content verbatim (the v2.0 JSON
// authoring document) — the editable draft behind a target. The
// resolution is eka-core's (Authoring.ResolveDraft); the file bytes
// are returned untouched. This is the renamed MCP tool (td:mcp-view-naming-fix).
func (c *Capability) DraftRead(target, project string) ([]byte, error) {
	ref, err := runtime.Authoring.ResolveDraft(c.rt, target, project)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(ref.Path)
	if err != nil {
		return nil, fmt.Errorf("eka: cannot read draft %s: %w", target, err)
	}
	return data, nil
}

// View is the deprecated alias of DraftRead (td:mcp-view-naming-fix).
// TODO(td:mcp-view-naming-fix): remove in next minor version after 1.1.3 — delete this method and keep DraftRead only.
func (c *Capability) View(target, project string) ([]byte, error) {
	return c.DraftRead(target, project)
}

// DraftList lists the draft backlog of one project (or every project
// when project is "") as a machine list (schema eka-draft-list-v1),
// ordered deterministically by eka-core's Authoring.Drafts.
func (c *Capability) DraftList(project string) ([]byte, error) {
	drafts, err := runtime.Authoring.Drafts(c.rt, project)
	if err != nil {
		return nil, err
	}
	items := make([]draftResult, 0, len(drafts))
	for _, d := range drafts {
		items = append(items, draftResult{
			Project:   d.Project,
			Namespace: d.Namespace,
			Type:      d.Type,
			ID:        d.ID,
			Path:      d.Path,
			Updated:   d.Updated,
		})
	}
	return json.Marshal(draftListResult{
		Schema: "eka-draft-list-v1",
		Count:  len(items),
		Drafts: items,
	})
}

// IntegrityCheck verifies the canonical store and returns the
// deterministic integrity report (schema eka-integrity-report-v1). The
// scan is entirely eka-core's (Integrity.Verify); this method only
// shapes the projection.
func (c *Capability) IntegrityCheck() ([]byte, error) {
	report, err := c.rt.Integrity.Verify()
	if err != nil {
		return nil, err
	}
	violations := make([]integrityViolation, 0, len(report.Violations))
	for _, v := range report.Violations {
		violations = append(violations, integrityViolation{
			Kind:    v.Kind,
			Subject: v.Subject,
			Detail:  v.Detail,
		})
	}
	return json.Marshal(integrityReport{
		Schema:             "eka-integrity-report-v1",
		PayloadsChecked:    report.PayloadsChecked,
		RefsChecked:        report.RefsChecked,
		AttachmentsChecked: report.AttachmentsChecked,
		OrphanPayloads:     report.OrphanPayloads,
		Violations:         violations,
	})
}

// SyncPush refreshes the repository snapshot from the workspace store
// (the push side of sync) — the same engine `eka sync push` uses
// (Authoring.Sync Pull:false, Push:true). The snapshot is the SOURCE
// transport (exchange.EmitSource) verified on every pull and emitted
// deterministically; the digest is the source fingerprint. Emission is
// crash-safe (staged in .snapshots-tmp, swapped atomically) so a
// failed push writes nothing partially (AC #2). Pull / --from-docs
// re-seed is deliberately NOT exposed — it re-points line references to
// older instances (silent regression hazard) and stays an
// operator-supervised CLI operation (`eka sync pull`).
//
// repoPath defaults to "." (the server cwd, same as the CLI). adopt
// corresponds to `eka sync push --adopt` (ADR-032 Option C2): workspace-
// native units (source_repo = "runtime") are re-attributed before the
// push, so a clone receives them. override is the machine override of
// the content-namespace reconciliation (ADR-020 Decision 3). Errors keep
// their refusal classes and are sanitized at the MCP boundary.
func (c *Capability) SyncPush(repoPath string, adopt, override bool) ([]byte, error) {
	if repoPath == "" {
		repoPath = "."
	}
	report, err := runtime.Authoring.Sync(c.rt, repoPath, runtime.SyncOptions{
		Pull:            false,
		Push:            true,
		AdoptBeforePush: adopt,
		Override:        override,
	})
	if err != nil {
		return nil, err
	}
	warnings := report.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	return json.Marshal(syncPushResult{
		Schema:            "eka-sync-push-result-v1",
		Workspace:         report.Workspace,
		Project:           report.Project,
		Repo:              report.Repo,
		PushedUnits:       report.PushedUnits,
		PushedAttachments: report.PushedAttachments,
		SnapshotLabel:     report.SnapshotLabel,
		SnapshotDigest:    report.SnapshotDigest,
		Changed:           report.PushChanged,
		NewRepo:           report.NewRepo,
		Warnings:          warnings,
	})
}

// Discard deletes one draft file without publishing (schema
// eka-discard-result-v1). The deletion is eka-core's
// (Authoring.DiscardDraft); the returned note names the project when
// the resolution fell back across projects.
func (c *Capability) Discard(target, project string) ([]byte, error) {
	note, err := runtime.Authoring.DiscardDraft(c.rt, target, project, false)
	if err != nil {
		return nil, err
	}
	return json.Marshal(discardResult{
		Schema: "eka-discard-result-v1",
		Target: target,
		Note:   note,
	})
}

// --- Wire shapes (the deterministic eka-*-v1 projections). ---

type conformanceReport struct {
	Schema       string              `json:"schema"`
	Root         string              `json:"root"`
	FilesScanned int                 `json:"filesScanned"`
	Artifacts    int                 `json:"artifacts"`
	Skipped      string              `json:"skipped"`
	Errors       int                 `json:"errors"`
	Warnings     int                 `json:"warnings"`
	Pass         bool                `json:"pass"`
	Results      []conformanceResult `json:"results"`
}

type conformanceResult struct {
	File     string `json:"file"`
	Rule     string `json:"rule"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type draftResult struct {
	Schema    string `json:"schema,omitempty"`
	Project   string `json:"project"`
	Namespace string `json:"namespace"`
	Type      string `json:"type"`
	ID        string `json:"id"`
	Path      string `json:"path"`
	Updated   string `json:"updated"`
}

type draftListResult struct {
	Schema string        `json:"schema"`
	Count  int           `json:"count"`
	Drafts []draftResult `json:"drafts"`
}

type publishResult struct {
	Schema          string `json:"schema"`
	Form            string `json:"form"`
	InstanceVersion int    `json:"instanceVersion"`
	ObjectHash      string `json:"objectHash"`
	Note            string `json:"note"`
}

type transitionResult struct {
	Schema         string                     `json:"schema"`
	Target         string                     `json:"target"`
	From           string                     `json:"from"`
	To             string                     `json:"to"`
	By             conformance.AuthorIdentity `json:"by"`
	ObjectHash     string                     `json:"objectHash"`
	LockedPlan     string                     `json:"lockedPlan"`
	LockedPlanHash string                     `json:"lockedPlanHash"`
	Warning        string                     `json:"warning"`
}

type noteResult struct {
	Schema       string                     `json:"schema"`
	ID           string                     `json:"id"`
	Target       string                     `json:"target"`
	SubjectState string                     `json:"subjectState"`
	Path         string                     `json:"path"`
	By           conformance.AuthorIdentity `json:"by"`
}

type discardResult struct {
	Schema string `json:"schema"`
	Target string `json:"target"`
	Note   string `json:"note"`
}

type integrityReport struct {
	Schema             string               `json:"schema"`
	PayloadsChecked    int                  `json:"payloadsChecked"`
	RefsChecked        int                  `json:"refsChecked"`
	AttachmentsChecked int                  `json:"attachmentsChecked"`
	OrphanPayloads     int                  `json:"orphanPayloads"`
	Violations         []integrityViolation `json:"violations"`
}

type integrityViolation struct {
	Kind    string `json:"kind"`
	Subject string `json:"subject"`
	Detail  string `json:"detail"`
}

type syncPushResult struct {
	Schema            string   `json:"schema"`
	Workspace         string   `json:"workspace"`
	Project           string   `json:"project"`
	Repo              string   `json:"repo"`
	PushedUnits       int      `json:"pushedUnits"`
	PushedAttachments int      `json:"pushedAttachments"`
	SnapshotLabel     string   `json:"snapshotLabel"`
	SnapshotDigest    string   `json:"snapshotDigest"`
	Changed           bool     `json:"changed"`
	NewRepo           bool     `json:"newRepo"`
	Warnings          []string `json:"warnings"`
}

// --- Helpers. ---

// stageContent writes the inline content object of a write tool to a
// temporary JSON file and returns its path ("" for nil content — the
// empty template scaffolds). The caller removes the file when done;
// eka-core reads it synchronously.
func stageContent(content map[string]any) (string, error) {
	if content == nil {
		return "", nil
	}
	f, err := os.CreateTemp("", "eka-mcp-content-*.json")
	if err != nil {
		return "", fmt.Errorf("eka: cannot stage content: %w", err)
	}
	path := f.Name()
	if err := json.NewEncoder(f).Encode(content); err != nil {
		f.Close()
		os.Remove(path)
		return "", fmt.Errorf("eka: cannot stage content: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", fmt.Errorf("eka: cannot stage content: %w", err)
	}
	return path, nil
}

// toAuthorIdentity maps the MCP boundary identity to the eka-core
// authoring identity (same kind/name model).
func toAuthorIdentity(a mcp.AuthorIdentity) conformance.AuthorIdentity {
	return conformance.AuthorIdentity{Kind: a.Kind, Name: a.Name}
}

// toRelationships maps the MCP boundary relationships to the eka-core
// exchange relationships (same type/target model).
func toRelationships(rels []mcp.Relationship) []exchange.Relationship {
	out := make([]exchange.Relationship, 0, len(rels))
	for _, r := range rels {
		out = append(out, exchange.Relationship{Type: r.Type, Target: r.Target})
	}
	return out
}
