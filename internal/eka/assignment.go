package eka

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/maleolabs/eka-core/conformance"
	"github.com/maleolabs/eka-core/exchange"
	"github.com/maleolabs/eka-core/metadata"
	"github.com/maleolabs/eka-core/runtime"
	"github.com/maleolabs/eka-core/store"
	"github.com/maleolabs/eka-core/view"
	"github.com/maleolabs/eka-core/workspace"
	"github.com/maleolabs/eka-mcp/internal/mcp"
)

// assignmentSchema is the schema id of the assignment machine report.
const assignmentSchema = "eka-assignment-v1"

// assignmentJSON is the deterministic machine report of one assignment run.
type assignmentJSON struct {
	Schema     string `json:"schema"`
	OK         bool   `json:"ok"`
	Action     string `json:"action"`
	Item       string `json:"item,omitempty"`
	Assignee   string `json:"assignee,omitempty"`
	NoAssignee bool   `json:"no-assignee,omitempty"`
	State      string `json:"state,omitempty"`
	ObjectHash string `json:"objectHash,omitempty"`
	By         string `json:"by,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Hint       string `json:"hint,omitempty"`
}

// assignmentAction discriminates the three assignment operations.
type assignmentAction string

const (
	actionAssign   assignmentAction = "assign"
	actionReassign assignmentAction = "reassign"
	actionUnassign assignmentAction = "unassign"
)

// Assign performs `eka assign` semantics through the Authoring API/store
// with explicit agent author identity. Deterministic refusals surface as
// errors with no partial writes.
func (c *Capability) Assign(req mcp.AssignmentRequest) ([]byte, error) {
	if strings.TrimSpace(req.Target) == "" {
		return nil, fmt.Errorf("assign requires {\"target\": string, \"to\": string}")
	}
	if strings.TrimSpace(req.To) == "" {
		return nil, fmt.Errorf("assign requires {\"target\": string, \"to\": string}")
	}
	return c.runAssignment(actionAssign, req.RepoPath, req.Target, req.To, req.By)
}

// Reassign performs `eka reassign` semantics.
func (c *Capability) Reassign(req mcp.AssignmentRequest) ([]byte, error) {
	if strings.TrimSpace(req.Target) == "" {
		return nil, fmt.Errorf("reassign requires {\"target\": string, \"to\": string}")
	}
	if strings.TrimSpace(req.To) == "" {
		return nil, fmt.Errorf("reassign requires {\"target\": string, \"to\": string}")
	}
	return c.runAssignment(actionReassign, req.RepoPath, req.Target, req.To, req.By)
}

// Unassign performs `eka unassign` semantics.
func (c *Capability) Unassign(req mcp.UnassignRequest) ([]byte, error) {
	if strings.TrimSpace(req.Target) == "" {
		return nil, fmt.Errorf("unassign requires {\"target\": string}")
	}
	return c.runAssignment(actionUnassign, req.RepoPath, req.Target, "", req.By)
}

func (c *Capability) runAssignment(action assignmentAction, repoPath, target, to string, by mcp.AuthorIdentity) ([]byte, error) {
	if repoPath == "" {
		repoPath = "."
	}
	// Ensure By is explicit agent identity (AC #2) — caller already resolved
	// via resolveAuthor. Defensive: if empty, use default agent.
	if by.Name == "" {
		by = mcp.AuthorIdentity{Kind: "agent", Name: "mcp-agent"}
	}
	if by.Kind == "" {
		by.Kind = "agent"
	}
	// Validate author kind deterministically.
	if !isAuthorKindMCP(by.Kind) {
		return nil, fmt.Errorf("unknown author kind %q (allowed: user, agent, worker)", by.Kind)
	}
	// Unassign with --to is usage error (mirror CLI).
	if action == actionUnassign && strings.TrimSpace(to) != "" {
		return nil, fmt.Errorf("unassign does not take \"to\": the assigned-to edge is removed, not re-pointed")
	}
	if (action == actionAssign || action == actionReassign) && strings.TrimSpace(to) == "" {
		return nil, fmt.Errorf("%s requires \"to\": the member line to assign to", action)
	}

	runtimeBy := toAuthorIdentity(by)

	ctx, err := resolveAssignmentTargetMCP(c.rt, repoPath, target)
	if err != nil {
		var refusal *assignmentRefusal
		if errors.As(err, &refusal) {
			return nil, fmt.Errorf("%s refused: %s; %s", action, refusal.reason, refusal.hint)
		}
		return nil, err
	}

	memberForm := ""
	if action == actionAssign || action == actionReassign {
		form, err := resolveMemberTarget(ctx, to)
		if err != nil {
			var refusal *assignmentRefusal
			if errors.As(err, &refusal) {
				return nil, fmt.Errorf("%s refused: %s; %s", action, refusal.reason, refusal.hint)
			}
			return nil, err
		}
		memberForm = form
	}

	raws := assignedToTargets(ctx.unit)
	if ctx.unit == nil && ctx.draftPath != "" {
		raws, err = draftAssignedToTargets(ctx.draftPath)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", action, err)
		}
	}
	members := assignedMembers(ctx.graph, ctx.ref.Namespace, raws)

	switch action {
	case actionAssign:
		if len(raws) > 0 {
			if len(members) > 0 && members[0] == memberForm {
				return marshalAssignment(assignmentJSON{
					Schema:   assignmentSchema,
					OK:       true,
					Action:   string(action),
					Item:     ctx.form,
					Assignee: memberForm,
					State:    "unchanged",
					By:       runtimeBy.Name,
				})
			}
			current := "an unresolvable member target"
			if len(members) > 0 && members[0] != "" {
				current = members[0]
			}
			return nil, fmt.Errorf("assign refused: %s is already assigned to %s; use 'eka reassign' to move the assignment", ctx.form, current)
		}
		res, err := runtime.Authoring.Relate(c.rt, runtime.RelateRequest{
			RepoPath: repoPath,
			Target:   ctx.form,
			Relationships: []exchange.Relationship{
				{Type: "assigned-to", Target: localMemberForm(memberForm)},
			},
		})
		if err != nil {
			var refusal *runtime.RelateRefusal
			if errors.As(err, &refusal) {
				return nil, fmt.Errorf("assign refused: %s; %s", refusal.Reason, refusal.Hint)
			}
			var ve *runtime.RelateValidationError
			if errors.As(err, &ve) {
				return nil, wrapValidationRefusal(fmt.Sprintf("assign refused: %s failed CKO-level validation with %d blocking error(s); nothing was changed", ve.Target, ve.Report.ErrorCount()), ve.Report)
			}
			return nil, err
		}
		return marshalAssignment(assignmentJSON{
			Schema:     assignmentSchema,
			OK:         true,
			Action:     string(action),
			Item:       res.Target,
			Assignee:   memberForm,
			State:      res.State,
			ObjectHash: res.ObjectHash,
			By:         runtimeBy.Name,
		})
	case actionReassign:
		if err := assignmentMissingGuardMCP(action, ctx); err != nil {
			return nil, err
		}
		if len(raws) == 0 {
			return nil, fmt.Errorf("reassign refused: %s is not assigned to any member; use 'eka assign' to set the first assignment", ctx.form)
		}
		if len(members) > 0 && members[0] == memberForm {
			return marshalAssignment(assignmentJSON{
				Schema:   assignmentSchema,
				OK:       true,
				Action:   string(action),
				Item:     ctx.form,
				Assignee: memberForm,
				State:    "unchanged",
				By:       runtimeBy.Name,
			})
		}
		state, hash, err := writeAssignmentMCP(ctx, memberForm)
		if err != nil {
			var refusal *assignmentRefusal
			if errors.As(err, &refusal) {
				return nil, fmt.Errorf("reassign refused: %s; %s", refusal.reason, refusal.hint)
			}
			var ve *assignmentValidationError
			if errors.As(err, &ve) {
				return nil, wrapValidationRefusal(fmt.Sprintf("reassign refused: %s failed CKO-level validation with %d blocking error(s); nothing was changed", ve.Target, ve.Report.ErrorCount()), ve.Report)
			}
			return nil, err
		}
		return marshalAssignment(assignmentJSON{
			Schema:     assignmentSchema,
			OK:         true,
			Action:     string(action),
			Item:       ctx.form,
			Assignee:   memberForm,
			State:      state,
			ObjectHash: hash,
			By:         runtimeBy.Name,
		})
	case actionUnassign:
		if err := assignmentMissingGuardMCP(action, ctx); err != nil {
			return nil, err
		}
		if len(raws) == 0 {
			return marshalAssignment(assignmentJSON{
				Schema:     assignmentSchema,
				OK:         true,
				Action:     string(action),
				Item:       ctx.form,
				NoAssignee: true,
				State:      "unchanged",
				By:         runtimeBy.Name,
			})
		}
		state, hash, err := writeAssignmentMCP(ctx, "")
		if err != nil {
			var refusal *assignmentRefusal
			if errors.As(err, &refusal) {
				return nil, fmt.Errorf("unassign refused: %s; %s", refusal.reason, refusal.hint)
			}
			var ve *assignmentValidationError
			if errors.As(err, &ve) {
				return nil, wrapValidationRefusal(fmt.Sprintf("unassign refused: %s failed CKO-level validation with %d blocking error(s); nothing was changed", ve.Target, ve.Report.ErrorCount()), ve.Report)
			}
			return nil, err
		}
		return marshalAssignment(assignmentJSON{
			Schema:     assignmentSchema,
			OK:         true,
			Action:     string(action),
			Item:       ctx.form,
			NoAssignee: true,
			State:      state,
			ObjectHash: hash,
			By:         runtimeBy.Name,
		})
	}
	return nil, fmt.Errorf("unknown assignment action %q", action)
}

func marshalAssignment(j assignmentJSON) ([]byte, error) {
	return json.Marshal(j)
}

func isAuthorKindMCP(kind string) bool {
	switch kind {
	case "user", "agent", "worker":
		return true
	}
	return false
}

// assignmentTarget mirrors CLI's assignmentTarget but with repoPath.
type assignmentTarget struct {
	ref             conformance.Reference
	form            string
	project         string
	units           []*exchange.Unit
	graph           *view.Graph
	unit            *exchange.Unit
	hasPendingDraft bool
	draftPath       string
}

type assignmentRefusal struct {
	reason string
	hint   string
}

func (e *assignmentRefusal) Error() string { return fmt.Sprintf("%s; %s", e.reason, e.hint) }

type assignmentValidationError struct {
	Target string
	Report *conformance.Report
}

func (e *assignmentValidationError) Error() string {
	return fmt.Sprintf("%s failed CKO-level validation with %d blocking error(s); nothing was changed", e.Target, e.Report.ErrorCount())
}

func resolveAssignmentTargetMCP(rt *runtime.Runtime, repoPath, target string) (*assignmentTarget, error) {
	ref, err := conformance.ParseReference(target, "", "")
	if err != nil {
		return nil, fmt.Errorf("invalid target %q: %v", target, err)
	}
	if ref.HasVersion {
		return nil, fmt.Errorf("%s is a canonical published form; assignment addresses the artifact line", target)
	}
	if !conformance.IsWorkItemType(ref.Type) {
		return nil, &assignmentRefusal{
			reason: fmt.Sprintf("%s is not a work item; assignment applies to work items (sto/ts/bug/td/ch/spk) only", target),
			hint:   "pass a work item line such as sto:<id>",
		}
	}
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve the working directory: %w", err)
	}
	abs = filepath.Clean(abs)
	meta, _, hasMeta, err := metadata.Find(abs)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve the repository context: %w", err)
	}
	if !hasMeta {
		return nil, &assignmentRefusal{
			reason: fmt.Sprintf("%s is not an EKA repository (no eka.yaml)", abs),
			hint:   "run 'eka init' first",
		}
	}
	repo, found, err := rt.Workspace.FindRepo(abs)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve the repository registration: %w", err)
	}
	if !found {
		return nil, &assignmentRefusal{
			reason: fmt.Sprintf("repository %s is not registered in the EKA workspace", abs),
			hint:   "run 'eka sync' (auto-registers) or 'eka project register' first",
		}
	}
	ns := meta.Namespace
	if ns == "" {
		ns = repo.Namespace
	}
	if ref.Namespace != "" {
		if ref.Namespace != ns {
			return nil, &assignmentRefusal{
				reason: fmt.Sprintf("target namespace %s differs from the repository namespace %s; cross-platform access is read-only", ref.Namespace, ns),
				hint:   "assign only artifacts of the repository's own namespace",
			}
		}
	} else {
		ref.Namespace = ns
	}
	units, err := rt.Knowledge.UnitsByProject(repo.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("cannot read the project knowledge: %w", err)
	}
	ctx := &assignmentTarget{
		ref:     ref,
		form:    view.LineForm(ref.Namespace, ref.Type, ref.ID),
		project: repo.ProjectID,
		units:   units,
		graph:   view.NewGraph(".", units),
		unit:    currentLineUnitMCP(units, ref),
	}
	if ctx.unit == nil {
		root, herr := workspace.HomeDir()
		if herr != nil {
			return nil, fmt.Errorf("cannot resolve the workspace root: %w", herr)
		}
		ctx.draftPath, ctx.hasPendingDraft, err = pendingDraftPathMCP(root, ctx.project, ref.Type, ref.ID)
		if err != nil {
			return nil, err
		}
	}
	return ctx, nil
}

func currentLineUnitMCP(units []*exchange.Unit, ref conformance.Reference) *exchange.Unit {
	var best *exchange.Unit
	for _, u := range units {
		if u.Identity.Namespace != ref.Namespace || u.Identity.Type != ref.Type || u.Identity.ID != ref.ID {
			continue
		}
		if best == nil || u.Identity.InstanceVersion > best.Identity.InstanceVersion {
			best = u
		}
	}
	return best
}

func pendingDraftPathMCP(wsRoot, project, typeToken, id string) (string, bool, error) {
	root := filepath.Join(wsRoot, "drafts", project)
	jsonPath := filepath.Join(root, typeToken+"-"+id+".json")
	if _, err := os.Stat(jsonPath); err == nil {
		return jsonPath, true, nil
	}
	if _, err := os.Stat(filepath.Join(root, typeToken+"-"+id+".md")); err == nil {
		return "", true, nil
	}
	return "", false, nil
}

func assignedToTargets(u *exchange.Unit) []string {
	if u == nil {
		return nil
	}
	var out []string
	for _, rel := range u.Relationships {
		if rel.Type == "assigned-to" {
			out = append(out, rel.Target)
		}
	}
	return out
}

func assignedMembers(g *view.Graph, ns string, raws []string) []string {
	out := make([]string, len(raws))
	for i, raw := range raws {
		ref, err := conformance.ParseReference(raw, ns, "sto")
		if err != nil {
			continue
		}
		u := g.Resolve(ref)
		if u == nil || u.Identity.Type != "mbr" {
			continue
		}
		out[i] = view.LineForm(u.Identity.Namespace, u.Identity.Type, u.Identity.ID)
	}
	return out
}

func assignmentMissingGuardMCP(action assignmentAction, ctx *assignmentTarget) error {
	if ctx.unit != nil || ctx.draftPath != "" {
		return nil
	}
	if ctx.hasPendingDraft {
		return &assignmentRefusal{
			reason: fmt.Sprintf("draft %s:%s is a legacy Markdown draft, which the assignment commands cannot mutate deterministically", ctx.ref.Type, ctx.ref.ID),
			hint:   "edit the file directly, or migrate the draft to the JSON format ('eka new' scaffolds JSON drafts)",
		}
	}
	// For CLI this is assignmentRefused with exit 1, but for MCP we return refusal error directly.
	return fmt.Errorf("%s refused: artifact line %s has no published instance and no pending draft; run 'eka new <type>:<id>' to scaffold a draft, or publish the pending draft first", action, ctx.form)
}

func resolveMemberTarget(ctx *assignmentTarget, raw string) (string, error) {
	form, err := parseMemberTarget(raw, ctx.ref.Namespace)
	if err != nil {
		return "", err
	}
	if !memberLineExists(ctx.units, form) && !draftMemberExists(ctx, form) {
		return "", &assignmentRefusal{
			reason: fmt.Sprintf("member %s does not resolve", form),
			hint:   fmt.Sprintf("available members of the repository: %s", strings.Join(memberLinesInNS(ctx.units, ctx.ref.Namespace), ", ")),
		}
	}
	return form, nil
}

func draftMemberExists(ctx *assignmentTarget, form string) bool {
	ref, err := conformance.ParseReference(form, ctx.ref.Namespace, "mbr")
	if err != nil {
		return false
	}
	root, err := workspace.HomeDir()
	if err != nil {
		return false
	}
	path := filepath.Join(root, "drafts", ctx.project, "mbr-"+ref.ID+".json")
	artifact, err := conformance.ScanFile(path)
	if err != nil || artifact == nil {
		return false
	}
	return artifact.Namespace == ctx.ref.Namespace
}

func parseMemberTarget(raw, itemNS string) (string, error) {
	t := strings.TrimSpace(raw)
	if t == "" {
		return "", &assignmentRefusal{reason: "a member target is required", hint: "pass --to <mbr-id>, mbr:<id>, mbr-<id>, or <ns>/mbr:<id>"}
	}
	if strings.Contains(t, "/") {
		ref, err := conformance.ParseReference(t, itemNS, "mbr")
		if err != nil {
			return "", &assignmentRefusal{reason: fmt.Sprintf("invalid member target %q", t), hint: "use <mbr-id>, mbr:<id>, mbr-<id>, or <ns>/mbr:<id>"}
		}
		if ref.Type != "mbr" {
			return "", &assignmentRefusal{reason: fmt.Sprintf("%q is not a member (mbr-) line", t), hint: "assignment points to a member line"}
		}
		if ref.Namespace != itemNS {
			return "", &assignmentRefusal{
				reason: fmt.Sprintf("member %s originates outside the work item's repository (%s); cross-repository assignment is refused", view.LineForm(ref.Namespace, "mbr", ref.ID), itemNS),
				hint:   "assign only members of the same repository",
			}
		}
		return view.LineForm(ref.Namespace, "mbr", ref.ID), nil
	}
	id := strings.TrimPrefix(strings.TrimPrefix(t, "mbr:"), "mbr-")
	if id == "" {
		return "", &assignmentRefusal{reason: fmt.Sprintf("invalid member target %q", t), hint: "use <mbr-id>, mbr:<id>, mbr-<id>, or <ns>/mbr:<id>"}
	}
	return view.LineForm(itemNS, "mbr", id), nil
}

func memberLines(units []*exchange.Unit) []string {
	seen := make(map[string]bool)
	var out []string
	for _, u := range units {
		if u.Identity.Type != "mbr" {
			continue
		}
		form := view.LineForm(u.Identity.Namespace, u.Identity.Type, u.Identity.ID)
		if seen[form] {
			continue
		}
		seen[form] = true
		out = append(out, form)
	}
	sort.Strings(out)
	return out
}

func memberLinesInNS(units []*exchange.Unit, ns string) []string {
	var out []string
	for _, form := range memberLines(units) {
		if strings.HasPrefix(form, ns+"/mbr:") {
			out = append(out, form)
		}
	}
	return out
}

func memberLineExists(units []*exchange.Unit, form string) bool {
	for _, u := range units {
		if u.Identity.Type == "mbr" && view.LineForm(u.Identity.Namespace, u.Identity.Type, u.Identity.ID) == form {
			return true
		}
	}
	return false
}

func localMemberForm(form string) string {
	if i := strings.Index(form, "/"); i >= 0 {
		return form[i+1:]
	}
	return form
}

func writeAssignmentMCP(ctx *assignmentTarget, assignee string) (string, string, error) {
	ws, err := workspace.Ensure()
	if err != nil {
		return "", "", err
	}
	defer ws.Close()
	st := ws.Store()
	line, err := st.UnitsByLine(ctx.ref.Namespace, ctx.ref.Type, ctx.ref.ID)
	if err != nil {
		return "", "", fmt.Errorf("assignment: %w", err)
	}
	if len(line) > 0 {
		return writeAssignmentPublishedMCP(st, line, ctx.form, localMemberForm(assignee))
	}
	return writeAssignmentDraftMCP(ws, ctx, localMemberForm(assignee))
}

func writeAssignmentPublishedMCP(st *store.Store, line []*exchange.Unit, lineForm, assignee string) (string, string, error) {
	current := line[0]
	for _, u := range line {
		if u.Identity.InstanceVersion > current.Identity.InstanceVersion {
			current = u
		}
	}
	next := *current
	next.Relationships = replaceAssignedTo(current.Relationships, assignee)
	next.Updated = time.Now().Format("2006-01-02")
	resolver := &assignmentStoreResolver{st: st}
	report, err := conformance.ValidateCKO(&next, conformance.ValidateCKOOptions{Resolve: resolver.Resolve})
	if err != nil {
		return "", "", fmt.Errorf("assignment: validation failed: %w", err)
	}
	report.Results = append(report.Results, resolver.Findings(next.CanonicalIdentityForm, next.StateVector.ContentState)...)
	if !report.Pass() {
		return "", "", &assignmentValidationError{Target: lineForm, Report: report}
	}
	curRef, ok, err := st.Ref(current.CanonicalIdentityForm)
	if err != nil {
		return "", "", fmt.Errorf("assignment: %w", err)
	}
	if !ok {
		return "", "", &assignmentRefusal{
			reason: fmt.Sprintf("the reference of %s is missing (store corruption)", current.CanonicalIdentityForm),
			hint:   "run 'eka integrity check'",
		}
	}
	unitJSON, err := exchange.MarshalUnit(&next)
	if err != nil {
		return "", "", fmt.Errorf("assignment: cannot serialize %s: %w", next.CanonicalIdentityForm, err)
	}
	hash, _, err := st.RepointUnit(unitJSON, next.ContentPayload, store.Ref{
		Form:            next.CanonicalIdentityForm,
		ProjectID:       curRef.ProjectID,
		SourceRepo:      curRef.SourceRepo,
		Namespace:       next.Identity.Namespace,
		Type:            next.Identity.Type,
		ID:              next.Identity.ID,
		InstanceVersion: next.Identity.InstanceVersion,
		Revision:        next.Revision,
		Dimension:       next.Classification.Dimension,
		Domain:          next.Classification.Domain,
		Phase:           next.Phase,
		UpdatedAt:       next.Updated,
	})
	if err != nil {
		return "", "", fmt.Errorf("assignment: cannot publish %s: %w", next.CanonicalIdentityForm, err)
	}
	return "published", hash, nil
}

func writeAssignmentDraftMCP(ws *workspace.Workspace, ctx *assignmentTarget, assignee string) (string, string, error) {
	if ctx.draftPath == "" {
		mdPath := filepath.Join(ws.Path(), "drafts", ctx.project, ctx.ref.Type+"-"+ctx.ref.ID+".md")
		if _, err := os.Stat(mdPath); err == nil {
			return "", "", &assignmentRefusal{
				reason: fmt.Sprintf("draft %s:%s is a legacy Markdown draft, which the assignment commands cannot mutate deterministically", ctx.ref.Type, ctx.ref.ID),
				hint:   "edit the file directly, or migrate the draft to the JSON format ('eka new' scaffolds JSON drafts)",
			}
		}
		return "", "", &assignmentRefusal{
			reason: fmt.Sprintf("artifact line %s has no published instance and no pending draft", ctx.form),
			hint:   "run 'eka new <type>:<id>' to scaffold a draft, or publish the pending draft first",
		}
	}
	if err := rewriteDraftAssignment(ctx.draftPath, assignee); err != nil {
		return "", "", err
	}
	rt, err := runtime.Ensure()
	if err != nil {
		return "", "", err
	}
	defer rt.Close()
	if _, err := runtime.Authoring.ValidateDraft(rt, ctx.ref.Type+":"+ctx.ref.ID, ctx.project); err != nil {
		return "", "", fmt.Errorf("assignment: %w", err)
	}
	return "draft", "", nil
}

func replaceAssignedTo(rels []exchange.Relationship, assignee string) []exchange.Relationship {
	type relKey struct{ t, target string }
	seen := make(map[relKey]bool)
	keys := make([]relKey, 0, len(rels)+1)
	for _, rel := range rels {
		if rel.Type == "assigned-to" {
			continue
		}
		k := relKey{t: rel.Type, target: strings.TrimSpace(rel.Target)}
		if k.target == "" || seen[k] {
			continue
		}
		seen[k] = true
		keys = append(keys, k)
	}
	if assignee != "" {
		k := relKey{t: "assigned-to", target: assignee}
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].t != keys[j].t {
			return keys[i].t < keys[j].t
		}
		return keys[i].target < keys[j].target
	})
	out := make([]exchange.Relationship, 0, len(keys))
	for _, k := range keys {
		out = append(out, exchange.Relationship{Type: k.t, Target: k.target})
	}
	return out
}

func draftAssignedToTargets(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read draft %s: %w", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("draft %s is not valid JSON: %w", path, err)
	}
	var out []string
	raw, ok := doc["relationships"].(map[string]any)
	if !ok {
		return nil, nil
	}
	targets, ok := raw[conformance.StateKeyCamel("assigned-to")].([]any)
	if !ok {
		return nil, nil
	}
	for _, t := range targets {
		if s, ok := t.(string); ok {
			out = append(out, s)
		}
	}
	return out, nil
}

func rewriteDraftAssignment(path, assignee string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("assignment: cannot read draft %s: %w", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("assignment: draft %s is not valid JSON: %w", path, err)
	}
	existing := draftRelationshipsOf(doc)
	merged := replaceAssignedTo(existing, assignee)
	rels := make(map[string][]string)
	for _, field := range conformance.RelationshipFieldNames() {
		var targets []string
		for _, rel := range merged {
			if rel.Type != field {
				continue
			}
			targets = append(targets, rel.Target)
		}
		if len(targets) > 0 {
			rels[conformance.StateKeyCamel(field)] = targets
		}
	}
	if len(rels) > 0 {
		doc["relationships"] = rels
	} else {
		delete(doc, "relationships")
	}
	out, err := json.Marshal(&doc)
	if err != nil {
		return fmt.Errorf("assignment: cannot serialize draft %s: %w", path, err)
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, out, "", "  "); err != nil {
		return fmt.Errorf("assignment: cannot serialize draft %s: %w", path, err)
	}
	indented.WriteByte('\n')
	return os.WriteFile(path, indented.Bytes(), 0o644)
}

func draftRelationshipsOf(doc map[string]any) []exchange.Relationship {
	var out []exchange.Relationship
	raw, ok := doc["relationships"].(map[string]any)
	if !ok {
		return out
	}
	for _, field := range conformance.RelationshipFieldNames() {
		targets, ok := raw[conformance.StateKeyCamel(field)].([]any)
		if !ok {
			continue
		}
		for _, t := range targets {
			if s, ok := t.(string); ok {
				out = append(out, exchange.Relationship{Type: field, Target: s})
			}
		}
	}
	return out
}

type assignmentStoreResolver struct {
	st   *store.Store
	errs map[string]error
}

func (r *assignmentStoreResolver) Resolve(ref conformance.Reference) bool {
	units, err := r.st.UnitsByLine(ref.Namespace, ref.Type, ref.ID)
	if err != nil {
		key := ref.Namespace + "/" + ref.Type + ":" + ref.ID
		if r.errs == nil {
			r.errs = make(map[string]error)
		}
		if _, seen := r.errs[key]; !seen {
			r.errs[key] = err
		}
		return false
	}
	if !ref.HasVersion {
		return len(units) > 0
	}
	for _, u := range units {
		if u.Identity.InstanceVersion == ref.Version {
			return true
		}
	}
	return false
}

func (r *assignmentStoreResolver) Findings(file, contentState string) []conformance.Result {
	if len(r.errs) == 0 {
		return nil
	}
	keys := make([]string, 0, len(r.errs))
	for k := range r.errs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]conformance.Result, 0, len(keys))
	for _, k := range keys {
		sev := conformance.SeverityError
		if contentState == "draft" {
			sev = conformance.SeverityWarning
		}
		out = append(out, conformance.Result{
			File:     file,
			Rule:     conformance.Rule5,
			Severity: sev,
			Message:  fmt.Sprintf("reference %s could not be checked against the store: %v", k, r.errs[k]),
		})
	}
	return out
}
