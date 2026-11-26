package mcp

// Retrieval-surface budget tests (sto:retrieval-surface-budget, M4).
//
// The advertised surface is the six grouped handles, not the flat member
// tools. These tests pin the budget so it cannot grow silently and prove that
// a group op and its flat alias run the identical handler.
//
// Measured tools/list cost (serialized advertised tool array, json.Marshal of
// the result the transport emits):
//
//	BEFORE (25 flat tools)  ToolCount=25 AdvertisedToolCount=26 schemaBytes=27981
//	AFTER  (6 group tools)  ToolCount=6  AdvertisedToolCount=6  schemaBytes=20503
//
// (20503 after the workspace assignment ops assign/reassign/unassign were
// retired from the workspace group, leaving sync_push; was 22158.)

import (
	"encoding/json"
	"strings"
	"testing"
)

// surfaceBudgetBytes is the recorded AFTER cost of the advertised tool array.
// A change that grows it without retiring something must fail here.
const surfaceBudgetBytes = 20503

// advertisedToolCount is the recorded AFTER number of advertised handles.
const advertisedToolCount = 6

// advertisedTools returns the tools/list entries keyed by advertised name.
func advertisedTools(t *testing.T, s *Server) map[string]map[string]any {
	t.Helper()
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tools := mustResult(t, out)["tools"].([]any)
	byName := make(map[string]map[string]any, len(tools))
	for _, tl := range tools {
		tm := tl.(map[string]any)
		byName[tm["name"].(string)] = tm
	}
	return byName
}

// groupSpecContaining returns the group that advertises a member capability.
func groupSpecContaining(member string) (groupSpec, bool) {
	for _, g := range groupSpecs {
		for _, m := range g.Members {
			if m == member {
				return g, true
			}
		}
	}
	return groupSpec{}, false
}

// advertisedGroupForMember returns the group spec and tools/list entry that
// advertise the member capability (flat names are aliases now).
func advertisedGroupForMember(t *testing.T, s *Server, member string) (groupSpec, map[string]any) {
	t.Helper()
	spec, ok := groupSpecContaining(member)
	if !ok {
		t.Fatalf("member %q belongs to no group", member)
	}
	entry, ok := advertisedTools(t, s)[spec.Name]
	if !ok {
		t.Fatalf("group %q is not advertised", spec.Name)
	}
	return spec, entry
}

// memberArgsProps returns the advertised `args.properties` for a member: the
// group's nested object schema is where the member's own properties live now.
func memberArgsProps(t *testing.T, s *Server, member string) map[string]any {
	t.Helper()
	_, entry := advertisedGroupForMember(t, s, member)
	schema := entry["inputSchema"].(map[string]any)
	props := schema["properties"].(map[string]any)
	args := props["args"].(map[string]any)
	return args["properties"].(map[string]any)
}

// memberGroupDescription returns the advertised description of the group that
// carries the member's verbatim capability description.
func memberGroupDescription(t *testing.T, s *Server, member string) string {
	t.Helper()
	_, entry := advertisedGroupForMember(t, s, member)
	return entry["description"].(string)
}

// TestSurfaceBudgetIsBounded (AC2) pins the advertised count and the exact
// schema byte cost. A new advertised tool/member without a retirement fails
// here; a deliberate change must update the recorded constants above.
func TestSurfaceBudgetIsBounded(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	tools := mustResult(t, out)["tools"].([]any)
	raw, err := json.Marshal(tools)
	if err != nil {
		t.Fatal(err)
	}
	if got := ToolCount(); got != advertisedToolCount {
		t.Errorf("ToolCount = %d, recorded budget %d: adding an advertised tool without retiring one grows the surface", got, advertisedToolCount)
	}
	if got := len(tools); got != advertisedToolCount {
		t.Errorf("tools/list entries = %d, recorded budget %d", got, advertisedToolCount)
	}
	if got := AdvertisedToolCount(); got != advertisedToolCount {
		t.Errorf("AdvertisedToolCount = %d, recorded budget %d", got, advertisedToolCount)
	}
	if got := len(raw); got != surfaceBudgetBytes {
		t.Errorf("advertised schema = %d bytes, recorded budget %d: adding a tool or member without retiring one grows the prefix cost (update the recorded budget only for a deliberate change)", got, surfaceBudgetBytes)
	}
}

// TestSurfaceGroupDescriptionsEmbedMemberTextVerbatim (AC1): every group
// description carries each member's existing description word-for-word under
// its op name, so discovery does not regress for an agent that sees only the
// group.
func TestSurfaceGroupDescriptionsEmbedMemberTextVerbatim(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	groups := advertisedTools(t, s)
	for _, g := range groupSpecs {
		entry, ok := groups[g.Name]
		if !ok {
			t.Errorf("group %q not advertised", g.Name)
			continue
		}
		desc := entry["description"].(string)
		for _, member := range g.Members {
			d := descriptorByName(member)
			if d == nil {
				t.Errorf("group %q lists unknown member %q", g.Name, member)
				continue
			}
			if !strings.Contains(desc, d.Description) {
				t.Errorf("group %q description must embed %q's description verbatim", g.Name, member)
			}
			if !strings.Contains(desc, `"`+member+`":`) {
				t.Errorf("group %q description must name op %q", g.Name, member)
			}
		}
	}
}

// TestSurfaceGroupRiskIsStrictestMember (AC1/gating): the group advertises the
// strictest member risk class so a client gating on the annotation is never
// under-warned.
func TestSurfaceGroupRiskIsStrictestMember(t *testing.T) {
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	groups := advertisedTools(t, s)
	want := map[string]string{
		"knowledge_read":  RiskRead,
		"knowledge_write": RiskCanonicalWrite,
		"code":            RiskRead,
		"workspace":       RiskCanonicalWrite,
		"ops":             RiskRead,
		"feedback":        RiskExternal,
	}
	for name, risk := range want {
		entry := groups[name]
		if entry == nil {
			t.Fatalf("group %q not advertised", name)
		}
		if got := RiskClassOf(name); got != risk {
			t.Errorf("RiskClassOf(%q) = %q, want strictest member risk %q", name, got, risk)
		}
		ann := entry["annotations"].(map[string]any)
		if ann["riskClass"] != risk {
			t.Errorf("group %q annotation riskClass = %v, want %q", name, ann["riskClass"], risk)
		}
		if ann["readOnlyHint"] != (risk == RiskRead) {
			t.Errorf("group %q readOnlyHint mismatch", name)
		}
		if ann["openWorldHint"] != (risk == RiskExternal) {
			t.Errorf("group %q openWorldHint mismatch", name)
		}
	}
}

// TestSurfaceGroupOpEqualsFlatAlias pins byte-identical content for the same
// args across the group envelope and the flat alias (read + write groups).
func TestSurfaceGroupOpEqualsFlatAlias(t *testing.T) {
	cases := []struct {
		name     string
		group    string
		op       string
		args     string
		flat     string
		flatArgs string
	}{
		{
			name:     "read get",
			group:    "knowledge_read",
			op:       "get",
			args:     `{"form":"feather/adr:001:1"}`,
			flat:     "get",
			flatArgs: `{"form":"feather/adr:001:1"}`,
		},
		{
			name:     "local write new",
			group:    "knowledge_write",
			op:       "new",
			args:     `{"project":"feather","namespace":"feather","type":"adr","id":"001"}`,
			flat:     "new",
			flatArgs: `{"project":"feather","namespace":"feather","type":"adr","id":"001"}`,
		},
		{
			name:     "canonical write publish",
			group:    "knowledge_write",
			op:       "publish",
			args:     `{"target":"feather/adr:001"}`,
			flat:     "publish",
			flatArgs: `{"target":"feather/adr:001"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(&fakeCapability{statusJSON: `{}`})
			groupMsg := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + tc.group + `","arguments":{"op":"` + tc.op + `","args":` + tc.args + `}}}`
			flatMsg := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + tc.flat + `","arguments":` + tc.flatArgs + `}}`
			g := mustResult(t, mustHandle(t, s, groupMsg))
			f := mustResult(t, mustHandle(t, s, flatMsg))
			if g["isError"] != f["isError"] {
				t.Fatalf("isError differs: group=%v flat=%v", g["isError"], f["isError"])
			}
			gt := g["content"].([]any)[0].(map[string]any)["text"].(string)
			ft := f["content"].([]any)[0].(map[string]any)["text"].(string)
			if gt != ft {
				t.Errorf("group op %q and flat %q must return byte-identical content, got\n%q\nvs\n%q", tc.op, tc.flat, gt, ft)
			}
		})
	}
}

// TestSurfaceGatingAppliesToGroupAndAlias pins that the canonical-write gate
// fires identically through the group op and the flat alias.
func TestSurfaceGatingAppliesToGroupAndAlias(t *testing.T) {
	t.Setenv("EKA_MCP_ALLOW_CANONICAL_WRITE", "")
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	groupMsg := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"knowledge_write","arguments":{"op":"publish","args":{"target":"feather/adr:001"}}}}`
	flatMsg := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"publish","arguments":{"target":"feather/adr:001"}}}`
	for _, msg := range []string{groupMsg, flatMsg} {
		res := mustResult(t, mustHandle(t, s, msg))
		if res["isError"] != true {
			t.Errorf("canonical write must be gated: %v", res)
			continue
		}
		text := res["content"].([]any)[0].(map[string]any)["text"].(string)
		if !strings.Contains(text, "EKA_MCP_ALLOW_CANONICAL_WRITE=1") {
			t.Errorf("gate refusal = %q, want approval instruction", text)
		}
	}
}

// TestSurfaceGroupOpRefusals pins the discriminator contract: missing/unknown
// op and a non-object args payload refuse deterministically.
func TestSurfaceGroupOpRefusals(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want string
	}{
		{"missing op", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"knowledge_read","arguments":{}}}`, `"op"`},
		{"unknown op", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"knowledge_read","arguments":{"op":"bogus"}}}`, `requires "op" to be one of`},
		{"args not an object", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"knowledge_read","arguments":{"op":"get","args":"bogus"}}}`, `requires "args" to be a JSON object`},
		{"member refusal forwarded", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"knowledge_read","arguments":{"op":"get","args":{}}}}`, `get requires "form"`},
	}
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := mustResult(t, mustHandle(t, s, tc.msg))
			if res["isError"] != true {
				t.Fatalf("expected a refusal, got %v", res)
			}
			text := res["content"].([]any)[0].(map[string]any)["text"].(string)
			if !strings.Contains(text, tc.want) {
				t.Errorf("refusal = %q, want %q", text, tc.want)
			}
		})
	}
}

// TestFlatNamesRemainDispatchAliases pins that every flat member descriptor is
// dispatchable even though it is not advertised (legacy clients keep working).
func TestFlatNamesRemainDispatchAliases(t *testing.T) {
	for _, d := range memberToolDescriptors {
		if _, ok := toolHandlers[d.Name]; !ok {
			t.Errorf("flat alias %q has no handler", d.Name)
		}
	}
	// view stays the pre-existing deprecated alias and still dispatches.
	s := newTestServer(&fakeCapability{statusJSON: `{}`})
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"view","arguments":{"target":"feather/adr:001"}}}`)
	if mustResult(t, out)["isError"] != false {
		t.Errorf("deprecated view alias must still dispatch")
	}
}
