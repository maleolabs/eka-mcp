package mcp

// Fuzz robustness — the server must survive arbitrary malformed input:
// 0 crashes, 0 hangs. Two Go fuzz targets (FuzzHandleMessage for the
// dispatch unit, FuzzServe for the transport) plus a deterministic
// corpus test (TestFuzzCorpus) that replays 10k+ generated malformed
// messages in every normal `go test` run, so CI exercises the fuzz
// surface without a fuzzing session.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"time"
)

// FuzzHandleMessage feeds arbitrary bytes to the dispatch unit: the
// server must never panic and every response must be valid JSON.
// Run locally with: go test -fuzz=FuzzHandleMessage -fuzztime=30s ./internal/mcp/
func FuzzHandleMessage(f *testing.F) {
	for _, s := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"ping"}`,
		`{not json`,
		`[]`,
		`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get","arguments":{"form":"x"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
		`null`,
		`"string"`,
		`123`,
		`{"jsonrpc":"2.0","id":{},"method":"ping"}`,
		`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"eka://status"}}`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		s := newTestServer(&fakeCapability{statusJSON: `{}`})
		resp := s.HandleMessage(data)
		if len(resp) == 0 {
			return
		}
		var v any
		if err := json.Unmarshal(resp, &v); err != nil {
			t.Fatalf("response is not valid JSON: %v\ninput: %q\nresp: %s", err, data, resp)
		}
	})
}

// FuzzServe feeds arbitrary bytes as the transport stream: Serve must
// terminate (no hang) and never panic.
// Run locally with: go test -fuzz=FuzzServe -fuzztime=30s ./internal/mcp/
func FuzzServe(f *testing.F) {
	for _, s := range []string{
		`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n",
		`{not json` + "\n",
		`[]` + "\n",
		"",
		"a",
		strings.Repeat("a", 100000) + "\n",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		s := newTestServer(&fakeCapability{statusJSON: `{}`})
		var out bytes.Buffer
		_ = s.Serve(bytes.NewReader(data), &out)
	})
}

// TestFuzzCorpus replays a deterministic corpus of 10k+ malformed and
// adversarial messages through the dispatch unit and the transport:
// 0 crashes, 0 hangs. The corpus is generated with a fixed seed, so the
// test is reproducible in CI.
func TestFuzzCorpus(t *testing.T) {
	msgs := generateCorpus(10000)
	s := newTestServer(&fakeCapability{statusJSON: `{}`})

	// Dispatch unit: no panic, valid JSON responses.
	for i, m := range msgs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("message %d panicked: %v\ninput: %q", i, r, m)
				}
			}()
			resp := s.HandleMessage(m)
			if len(resp) > 0 {
				var v any
				if err := json.Unmarshal(resp, &v); err != nil {
					t.Fatalf("message %d produced invalid JSON: %v\ninput: %q\nresp: %s", i, err, m, resp)
				}
			}
		}()
	}

	// Transport: replay the corpus as one stream; Serve must terminate
	// (no hang) and never panic.
	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("transport panicked: %v", r)
			}
		}()
		var out bytes.Buffer
		done <- s.Serve(bytes.NewReader(bytes.Join(msgs, []byte("\n"))), &out)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve failed on the malformed corpus: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Serve hung on the malformed corpus")
	}
}

// generateCorpus builds a deterministic corpus of malformed and
// adversarial messages: fixed hostile shapes plus seeded random
// mutations of valid messages and random JSON-ish garbage.
func generateCorpus(n int) [][]byte {
	rng := rand.New(rand.NewSource(42))
	base := []string{
		`{"jsonrpc":"2.0","id":1,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"get","arguments":{"form":"x/adr:y:1"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"resources/read","params":{"uri":"eka://status"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"feedback_new","arguments":{"type":"bug","title":"fuzz"}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"feedback_list","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"feedback_publish","arguments":{"id":"fbk-20260812-test"}}}`,
	}
	fixed := []string{
		``, `{`, `}`, `[`, `]`, `[[]`, `{}`, `[]`, `null`, `true`, `false`, `0`, `-1`, `1.5`, `"str"`,
		`{"jsonrpc":"2.0"}`, `{"jsonrpc":"2.0","id":1}`, `{"jsonrpc":"2.0","method":123}`,
		`{"jsonrpc":1,"id":1,"method":"ping"}`, `{"jsonrpc":"2.0","id":{},"method":"ping"}`,
		`{"jsonrpc":"2.0","id":[],"method":"ping"}`, `{"jsonrpc":"2.0","id":true,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":1,"method":"ping","params":123}`,
		`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`, `[{},{}]`, `[null]`,
		`{"jsonrpc":"2.0","id":1,"method":"ping"} trailing`,
		`{"jsonrpc":"2.0","id":1,"method":"ping"}{"jsonrpc":"2.0","id":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":1,"method":"` + strings.Repeat("x", 100000) + `"}`,
		`{"jsonrpc":"2.0","id":1,"method":"ping","params":` + strings.Repeat("[", 1000) + strings.Repeat("]", 1000) + `}`,
		`{"jsonrpc":"2.0","id":1,"method":"ping","params":{"a":` + strings.Repeat("1", 10000) + `}}`,
		"{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"ping\"}\x00\x01\x02",
		`{"jsonrpc":"2.0","id":1,"method":"ping","params":"` + strings.Repeat("\\u0000", 100) + `"}`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":` + strings.Repeat("9", 1000) + `}}`,
	}
	out := make([][]byte, 0, n)
	for _, s := range fixed {
		out = append(out, []byte(s))
	}
	for len(out) < n {
		mut := []byte(base[rng.Intn(len(base))])
		for k := 0; k < 1+rng.Intn(4); k++ {
			switch rng.Intn(6) {
			case 0: // truncate
				if len(mut) > 0 {
					mut = mut[:rng.Intn(len(mut))]
				}
			case 1: // flip a byte
				if len(mut) > 0 {
					mut[rng.Intn(len(mut))] = byte(rng.Intn(256))
				}
			case 2: // insert random bytes
				pos := rng.Intn(len(mut) + 1)
				ins := make([]byte, rng.Intn(16))
				rng.Read(ins)
				mut = append(mut[:pos], append(ins, mut[pos:]...)...)
			case 3: // random garbage
				g := make([]byte, rng.Intn(64))
				rng.Read(g)
				mut = g
			case 4: // duplicate a chunk
				if len(mut) > 0 {
					pos := rng.Intn(len(mut))
					end := pos + rng.Intn(len(mut)-pos)
					mut = append(mut, mut[pos:end]...)
				}
			case 5: // random JSON-ish garbage
				mut = []byte(randomJSONish(rng))
			}
		}
		out = append(out, mut)
	}
	return out
}

// randomJSONish builds a random, possibly malformed JSON document.
func randomJSONish(rng *rand.Rand) string {
	var b strings.Builder
	writeJSONish(&b, rng, rng.Intn(6))
	return b.String()
}

func writeJSONish(b *strings.Builder, rng *rand.Rand, depth int) {
	switch rng.Intn(6) {
	case 0:
		b.WriteString(`"`)
		b.WriteString(randomString(rng, rng.Intn(20)))
		b.WriteString(`"`)
	case 1:
		b.WriteString(strconv.FormatInt(rng.Int63(), 10))
	case 2:
		b.WriteString("null")
	case 3:
		b.WriteString("true")
	case 4:
		if depth <= 0 {
			b.WriteString("{}")
			return
		}
		b.WriteString(`{"`)
		b.WriteString(randomString(rng, 5))
		b.WriteString(`":`)
		writeJSONish(b, rng, depth-1)
		b.WriteString(`}`)
	case 5:
		if depth <= 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString(`[`)
		for i := 0; i < rng.Intn(4); i++ {
			if i > 0 {
				b.WriteString(`,`)
			}
			writeJSONish(b, rng, depth-1)
		}
		b.WriteString(`]`)
	}
}

func randomString(rng *rand.Rand, n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789_/\\:\"{}[]"
	out := make([]byte, n)
	for i := range out {
		out[i] = chars[rng.Intn(len(chars))]
	}
	return string(out)
}
