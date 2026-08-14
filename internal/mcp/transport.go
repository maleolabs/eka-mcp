package mcp

import (
	"bufio"
	"bytes"
	"io"
)

// Serve runs the MCP server over a stdio-style byte stream: it reads
// newline-delimited JSON-RPC messages from r (the MCP stdio framing —
// one JSON message per line, no embedded newlines) and writes the
// response messages to w. It returns when the input stream ends
// (io.EOF — the client closed the pipe), or on a transport error.
//
// The writer is flushed after every response so the client observes
// each reply immediately.
func (s *Server) Serve(r io.Reader, w io.Writer) error {
	br := bufio.NewReader(r)
	bw := bufio.NewWriter(w)
	for {
		line, err := readLine(br)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		resp := s.HandleMessage(line)
		if len(resp) == 0 {
			continue
		}
		if _, err := bw.Write(resp); err != nil {
			return err
		}
		if err := bw.WriteByte('\n'); err != nil {
			return err
		}
		if err := bw.Flush(); err != nil {
			return err
		}
	}
}

// readLine reads one newline-terminated line. bufio.Reader.ReadBytes
// grows its read buffer as needed, so a message is never truncated at
// an arbitrary size (knowledge payloads can be large and are served
// whole). ReadBytes returns err != nil exactly when the returned data
// does not end in the delimiter — so a final partial line (EOF without
// a trailing newline) is still delivered, with the EOF surfaced on the
// next call.
func readLine(br *bufio.Reader) ([]byte, error) {
	line, err := br.ReadBytes('\n')
	if err != nil && len(bytes.TrimSpace(line)) > 0 {
		// Final partial line: deliver it now, defer the EOF to the next
		// call so Serve processes it before terminating.
		return line, nil
	}
	return line, err
}
