// Package eka implements the EKA capability layer of eka-mcp: the thin
// adapter between the MCP server and the eka-core Runtime.
//
// It wraps the runtime services (Resolver, Knowledge, Workspace) and the
// deterministic machine projections (machine.NewDocument,
// machine.NewCollection) behind the retrieval surface the MCP server
// dispatches to. It deliberately reimplements NO EKA domain logic — every
// call delegates to eka-core; this package only shapes the wire responses.
package eka

import (
	"encoding/json"
	"fmt"

	"github.com/maleolabs/eka-core/machine"
	"github.com/maleolabs/eka-core/runtime"
)

// Capability is one opened EKA capability: a read-only handle on the
// EKA Runtime exposing the knowledge-retrieval surface (identity
// resolution, domain collections, workspace status).
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
func (c *Capability) Get(form string) ([]byte, error) {
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
func (c *Capability) Status() ([]byte, error) {
	st, err := c.rt.Workspace.Status()
	if err != nil {
		return nil, err
	}
	return json.Marshal(st)
}
