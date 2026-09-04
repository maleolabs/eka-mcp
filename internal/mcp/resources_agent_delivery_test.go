package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	pack "github.com/maleolabs/eka-mcp"
)

// TestResourcesManifestAndBootstrap verifies the compact manifest/index and
// bootstrap resources: they are present in resources/list, have the right
// mime types, carry annotations, and are readable with deterministic content.
func TestResourcesManifestAndBootstrap(t *testing.T) {
	s := conformanceServer()
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`)
	res := mustResult(t, out)
	resources := res["resources"].([]any)
	byURI := map[string]map[string]any{}
	for _, r := range resources {
		m := r.(map[string]any)
		byURI[m["uri"].(string)] = m
	}
	for _, want := range []struct {
		uri      string
		mimeType string
		contains string
	}{
		{pack.ManifestURI, "application/json", "eka-pack-manifest-v1"},
		{pack.BootstrapURI, "text/markdown", "EKA Bootstrap"},
	} {
		entry, ok := byURI[want.uri]
		if !ok {
			t.Fatalf("resources/list missing %s", want.uri)
		}
		if entry["mimeType"] != want.mimeType {
			t.Errorf("%s mimeType = %v, want %v", want.uri, entry["mimeType"], want.mimeType)
		}
		if entry["annotations"] == nil {
			t.Errorf("%s must carry annotations", want.uri)
		}
		out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"`+want.uri+`"}}`)
		res := mustResult(t, out)
		contents := res["contents"].([]any)[0].(map[string]any)
		text := contents["text"].(string)
		if !strings.Contains(text, want.contains) {
			t.Errorf("%s text must contain %q, got %q", want.uri, want.contains, text[:500])
		}
		if contents["mimeType"] != want.mimeType {
			t.Errorf("%s read mimeType = %v, want %v", want.uri, contents["mimeType"], want.mimeType)
		}
	}
	// Manifest must be compact and carry skills/templates/commands.
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"eka://manifest"}}`)
	manifestText := mustResult(t, out)["contents"].([]any)[0].(map[string]any)["text"].(string)
	var manifest map[string]any
	if err := json.Unmarshal([]byte(manifestText), &manifest); err != nil {
		t.Fatalf("manifest is not JSON: %v", err)
	}
	if manifest["schema"] != "eka-pack-manifest-v1" {
		t.Errorf("manifest schema = %v, want eka-pack-manifest-v1", manifest["schema"])
	}
	for _, k := range []string{"skills", "templates", "commands"} {
		if manifest[k] == nil {
			t.Errorf("manifest missing %q", k)
		}
	}
	if len(manifestText) > 8000 {
		t.Errorf("manifest is not compact: %d bytes", len(manifestText))
	}
	// Bootstrap must document lazy load order and fallback.
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"eka://bootstrap"}}`)
	bootstrapText := mustResult(t, out)["contents"].([]any)[0].(map[string]any)["text"].(string)
	for _, want := range []string{"eka://manifest", "lazy", "Fallback", "versioned", pack.Version} {
		if !strings.Contains(bootstrapText, want) && !strings.Contains(strings.ToLower(bootstrapText), strings.ToLower(want)) {
			t.Errorf("bootstrap must mention %q", want)
		}
	}
}

// TestResourcesLazyVersionedReads verifies lazy versioned skill/template/command reads:
// unversioned and current-versioned reads succeed and are byte-identical;
// unknown versions refuse with -32002 and a fallback hint.
func TestResourcesLazyVersionedReads(t *testing.T) {
	s := conformanceServer()
	skills := mustSkillDirs(t)
	if len(skills) == 0 {
		t.Fatal("no skills embedded")
	}
	types := mustTemplateTypes(t)
	commands := mustCommandFiles(t)

	cases := []struct {
		uri string
	}{
		{pack.SkillsPrefix + skills[0]},
		{pack.TemplatesPrefix + types[0]},
	}
	if len(commands) > 0 {
		cases = append(cases, struct{ uri string }{pack.CommandsPrefix + commands[0]})
	}
	cases = append(cases, struct{ uri string }{pack.ManifestURI}, struct{ uri string }{pack.BootstrapURI})

	for _, tc := range cases {
		// Unversioned read succeeds.
		out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"`+tc.uri+`"}}`)
		res := mustResult(t, out)
		text := res["contents"].([]any)[0].(map[string]any)["text"].(string)
		if text == "" {
			t.Errorf("%s unversioned read empty", tc.uri)
		}
		// Current-versioned read succeeds and is byte-identical (lazy versioned).
		versioned := tc.uri + "@" + pack.Version
		out2 := mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"`+versioned+`"}}`)
		res2 := mustResult(t, out2)
		text2 := res2["contents"].([]any)[0].(map[string]any)["text"].(string)
		if text != text2 {
			t.Errorf("%s versioned read must be byte-identical to unversioned", tc.uri)
		}
		// Pack version (manifest version) also accepted when different.
		info, _ := pack.ReadPackInfo()
		if info.Version != pack.Version {
			v2 := tc.uri + "@" + info.Version
			out3 := mustHandle(t, s, `{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"`+v2+`"}}`)
			if out3["error"] != nil {
				t.Errorf("%s version %s should succeed (pack version), got error %v", tc.uri, info.Version, out3["error"])
			}
		}
		// Unknown version refuses with resource not found and fallback hint.
		bogus := tc.uri + "@9.9.9-unknown"
		out4 := mustHandle(t, s, `{"jsonrpc":"2.0","id":4,"method":"resources/read","params":{"uri":"`+bogus+`"}}`)
		if out4["error"] == nil {
			t.Fatalf("%s unknown version must error, got %v", bogus, out4)
		}
		errObj := out4["error"].(map[string]any)
		if errObj["code"] != float64(codeResourceNotFound) {
			t.Errorf("%s unknown version code = %v, want %v", bogus, errObj["code"], codeResourceNotFound)
		}
		msg := errObj["message"].(string)
		if !strings.Contains(msg, "fallback") && !strings.Contains(msg, "unversioned") {
			t.Errorf("unknown version error must mention fallback, got %q", msg)
		}
		if !strings.Contains(msg, pack.Version) {
			t.Errorf("unknown version error must mention available %q, got %q", pack.Version, msg)
		}
	}
}

// TestResourcesCommandsExposed verifies that command files are exposed as
// resources with correct mime types and frontmatter descriptions.
func TestResourcesCommandsExposed(t *testing.T) {
	s := conformanceServer()
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`)
	res := mustResult(t, out)
	byURI := map[string]map[string]any{}
	for _, r := range res["resources"].([]any) {
		m := r.(map[string]any)
		byURI[m["uri"].(string)] = m
	}
	files := mustCommandFiles(t)
	for _, f := range files {
		uri := pack.CommandsPrefix + f
		entry, ok := byURI[uri]
		if !ok {
			t.Errorf("resources/list missing %s", uri)
			continue
		}
		if entry["mimeType"] != "text/markdown" {
			t.Errorf("%s mimeType = %v, want text/markdown", uri, entry["mimeType"])
		}
		if entry["description"] == nil || entry["description"] == "" {
			t.Errorf("%s must carry frontmatter description", uri)
		}
		out := mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"`+uri+`"}}`)
		res := mustResult(t, out)
		text := res["contents"].([]any)[0].(map[string]any)["text"].(string)
		if !strings.Contains(text, "---") {
			t.Errorf("%s read must be markdown with frontmatter, got %q", uri, text[:100])
		}
		// Versioned command read also succeeds.
		out2 := mustHandle(t, s, `{"jsonrpc":"2.0","id":3,"method":"resources/read","params":{"uri":"`+uri+`@`+pack.Version+`"}}`)
		if out2["error"] != nil {
			t.Errorf("versioned %s must succeed, got %v", uri, out2["error"])
		}
	}
}

// TestResourcesAnnotations pins the annotation contract for each resource family.
func TestResourcesAnnotations(t *testing.T) {
	s := conformanceServer()
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`)
	resources := mustResult(t, out)["resources"].([]any)
	for _, r := range resources {
		m := r.(map[string]any)
		ann, ok := m["annotations"].(map[string]any)
		if !ok || ann == nil {
			t.Errorf("resource %v missing annotations", m["uri"])
			continue
		}
		if ann["priority"] == nil || ann["audience"] == nil {
			t.Errorf("resource %v annotations = %v, want priority and audience", m["uri"], ann)
		}
		// Manifest and bootstrap are highest priority.
		uri := m["uri"].(string)
		pri, _ := ann["priority"].(float64)
		if uri == pack.ManifestURI || uri == pack.BootstrapURI {
			if pri < 0.9 {
				t.Errorf("%s priority = %v, want >=0.9", uri, pri)
			}
		}
	}
}

// TestResourcesFallbackDocs verifies that the fallback guidance is present
// in the bootstrap and that unknown resources refuse with a helpful message
// rather than leaking paths.
func TestResourcesFallbackDocs(t *testing.T) {
	s := conformanceServer()
	// Bootstrap must document fallback for missing resources, version mismatch and offline install.
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"eka://bootstrap"}}`)
	text := mustResult(t, out)["contents"].([]any)[0].(map[string]any)["text"].(string)
	for _, want := range []string{"eka://manifest", "unversioned", "install"} {
		if !strings.Contains(strings.ToLower(text), strings.ToLower(want)) {
			t.Errorf("bootstrap must document %q fallback", want)
		}
	}
	// Unknown resource refuses deterministically.
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"resources/read","params":{"uri":"eka://skills/bogus-skill"}}`)
	if out["error"] == nil {
		t.Fatal("unknown skill must error")
	}
	if out["error"].(map[string]any)["code"] != float64(codeResourceNotFound) {
		t.Errorf("unknown skill code = %v, want %v", out["error"].(map[string]any)["code"], codeResourceNotFound)
	}
}

// TestGuidanceRemainsResourceContentAndOperationsRemainTools ensures the
// separation: guidance (skills, commands, templates, manifest, bootstrap)
// is always resource content, never a tool; operations remain tools.
func TestGuidanceRemainsResourceContentAndOperationsRemainTools(t *testing.T) {
	s := conformanceServer()
	// Resources/list must contain guidance URIs.
	out := mustHandle(t, s, `{"jsonrpc":"2.0","id":1,"method":"resources/list"}`)
	uris := map[string]bool{}
	for _, r := range mustResult(t, out)["resources"].([]any) {
		uris[r.(map[string]any)["uri"].(string)] = true
	}
	for _, prefix := range []string{pack.SkillsPrefix, pack.CommandsPrefix, pack.TemplatesPrefix} {
		found := false
		for u := range uris {
			if strings.HasPrefix(u, prefix) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("resources must contain guidance prefix %q", prefix)
		}
	}
	// Tools/list must contain operations, but must NOT contain guidance fetchers as tools.
	out = mustHandle(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	tools := map[string]bool{}
	for _, tl := range mustResult(t, out)["tools"].([]any) {
		tools[tl.(map[string]any)["name"].(string)] = true
	}
	// Operations remain tools.
	for _, op := range []string{"context", "get", "domain", "status", "validate", "new", "publish", "transition", "draft_read"} {
		if !tools[op] {
			t.Errorf("tools/list must still contain operation %q", op)
		}
	}
	// Guidance fetchers must NOT be tools.
	for _, forbidden := range []string{"skill_get", "template_get", "command_get", "manifest", "bootstrap"} {
		if tools[forbidden] {
			t.Errorf("tools/list must NOT contain guidance tool %q (guidance remains resource content)", forbidden)
		}
	}
}
