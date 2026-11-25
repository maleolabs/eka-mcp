package pack

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// skillsManifest is the pack manifest shipped alongside the MCP server.
const skillsManifest = "skills/manifest.yaml"

// requiresEkaPattern matches the pack's declared CLI requirement, e.g.
// 'requiresEka: ">= 1.16.0"'.
var requiresEkaPattern = regexp.MustCompile(`requiresEka:\s*">=\s*([0-9]+\.[0-9]+\.[0-9]+)"`)

// projectVersionPattern matches the release version anvil.yaml declares, e.g.
// '    version: 1.15.2'. Only the project block carries a bare three-component
// version, so the indentation anchor is what keeps this unambiguous.
var projectVersionPattern = regexp.MustCompile(`(?m)^\s+version:\s*"?([0-9]+\.[0-9]+\.[0-9]+)"?\s*$`)

// TestPackVersionMatchesProjectVersion fails when the version the binary
// reports and the version the release pipeline tags disagree.
//
// The release identity lives in three places, and each answers a different
// question: anvil.yaml is what the release workflow validates the tag against
// (so it decides what a release IS), Version in pack.go is what the running
// server reports over MCP, and skills/manifest.yaml requiresEka is what the
// pack promises about the CLI it ships inside. The last two are covered by the
// tests above; the first was not, so a half-done release (core tagged, plugin
// version left at the previous value) reported one version while CI shipped
// another and nothing failed. This test closes that axis: the three copies are
// now all locked to a checked source.
func TestPackVersionMatchesProjectVersion(t *testing.T) {
	raw, err := os.ReadFile("anvil.yaml")
	if err != nil {
		t.Fatalf("anvil.yaml is unreadable: %v", err)
	}
	m := projectVersionPattern.FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatal("anvil.yaml no longer declares the project version as 'version: X.Y.Z'")
	}
	if released := m[1]; strings.TrimPrefix(Version, "v") != released {
		t.Errorf("pack.go reports version %s, anvil.yaml releases %s: the release workflow tags one version and the binary claims another", Version, released)
	}
}

// TestSkillPackRequiresEkaNotBeyondRelease fails when the pack advertises a
// CLI requirement its own release does not satisfy.
//
// The requirement is a promise about what the pack's command references work
// against. It drifted once: the manifest declared ">= 1.16.0" while the
// shipping server was 1.15.1, so following the manifest's own instruction
// ("check the local CLI against requiresEka before trusting the pack") led a
// user to reject a release that actually satisfied it.
//
// The pack ships inside the MCP release, so the honest floor is the release
// version itself: the command surface described by the pack is the one that
// release provides. Anything higher is a promise the release cannot keep.
func TestSkillPackRequiresEkaNotBeyondRelease(t *testing.T) {
	raw, err := os.ReadFile(skillsManifest)
	if err != nil {
		t.Fatalf("%s is unreadable: %v", skillsManifest, err)
	}
	m := requiresEkaPattern.FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatalf("%s no longer declares a requiresEka floor as 'requiresEka: \">= X.Y.Z\"'", skillsManifest)
	}
	required := m[1]

	shipping := strings.TrimPrefix(Version, "v")
	if cmp, ok := compareSemver(required, shipping); ok && cmp > 0 {
		t.Errorf(
			"skill pack requires eka >= %s but ships inside eka-mcp %s: "+
				"following the manifest's own instruction would reject the release that contains it",
			required, shipping)
	}
}

// TestSkillPackRequirementIsCurrent pins the floor to the shipping release, so
// the value cannot trail the pack it describes either. A trailing floor is
// harmless to a user, but it means the manifest was not updated with the pack
// — and the next drift is then the harmful direction.
func TestSkillPackRequirementIsCurrent(t *testing.T) {
	raw, err := os.ReadFile(skillsManifest)
	if err != nil {
		t.Fatalf("%s is unreadable: %v", skillsManifest, err)
	}
	m := requiresEkaPattern.FindStringSubmatch(string(raw))
	if m == nil {
		return // reported by the test above
	}
	required := m[1]
	shipping := strings.TrimPrefix(Version, "v")
	if cmp, ok := compareSemver(required, shipping); ok && cmp != 0 {
		t.Errorf("skill pack requires eka >= %s, shipping release is %s; the requirement must track the release", required, shipping)
	}
}

// compareSemver compares two three-component versions numerically. The second
// return value is false when either input is not three numeric components.
func compareSemver(a, b string) (int, bool) {
	pa, okA := semverParts(a)
	pb, okB := semverParts(b)
	if !okA || !okB {
		return 0, false
	}
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1, true
			}
			return 1, true
		}
	}
	return 0, true
}

// semverParts splits a three-component version into integers.
func semverParts(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
