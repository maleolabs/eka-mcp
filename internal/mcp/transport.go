package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// defaultMaxLineSize is the hard cap on one stdio message line (64 MiB).
// A larger line is refused deterministically and the stream is
// resynchronized at the next newline, so one oversized message cannot
// wedge the session or exhaust the process memory.
const defaultMaxLineSize = 64 << 20

// errLineTooLong marks a message line that exceeded the line cap.
var errLineTooLong = errors.New("mcp: message exceeds the 64 MiB line limit")

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
		line, err := readLine(br, s.maxLineSize)
		if err == io.EOF {
			return nil
		}
		if err == errLineTooLong {
			if werr := writeResponse(bw, lineTooLongResponse()); werr != nil {
				return werr
			}
			continue
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
		if err := writeResponse(bw, resp); err != nil {
			return err
		}
	}
}

// writeResponse writes one response line and flushes it, so the client
// observes each reply immediately (the MCP stdio framing contract:
// newline-delimited, flush per response).
func writeResponse(bw *bufio.Writer, resp []byte) error {
	if _, err := bw.Write(resp); err != nil {
		return err
	}
	if err := bw.WriteByte('\n'); err != nil {
		return err
	}
	return bw.Flush()
}

// lineTooLongResponse is the deterministic refusal for a message line
// exceeding the line cap: a JSON-RPC invalid-request error with a fixed
// message (no internal details).
func lineTooLongResponse() []byte {
	return marshalResponse(response{
		JSONRPC: "2.0",
		ID:      json.RawMessage("null"),
		Error:   &rpcError{Code: codeInvalidRequest, Message: "message exceeds the 64 MiB line limit"},
	})
}

// readLine reads one newline-terminated line, accumulating fragments up
// to max (the cap counts the message content, not the framing newline).
// A line that exceeds the cap is drained (the stream is resynchronized
// at the next newline) and reported as errLineTooLong. A final partial
// line (EOF without a trailing newline) is delivered with the EOF
// deferred to the next call, so Serve processes it before terminating.
func readLine(br *bufio.Reader, max int) ([]byte, error) {
	var buf []byte
	for {
		frag, err := br.ReadSlice('\n')
		if err == nil {
			// Complete line: frag includes the framing newline, so the
			// message content is len(buf)+len(frag)-1 bytes.
			if len(buf)+len(frag)-1 > max {
				return nil, errLineTooLong
			}
			return append(buf, frag...), nil
		}
		if err == bufio.ErrBufferFull {
			// Fragment without a newline: the message already exceeds
			// the cap — drain to the next newline and refuse. Note the
			// transient buf capacity can reach ~max plus one read
			// buffer before the refusal fires (up to ~2x max at the
			// boundary with a buffer the size of the cap).
			if len(buf)+len(frag) > max {
				drainLine(br)
				return nil, errLineTooLong
			}
			buf = append(buf, frag...)
			continue
		}
		// EOF or reader error.
		if len(buf)+len(frag) > max {
			return nil, errLineTooLong
		}
		line := append(buf, frag...)
		if err == io.EOF && len(bytes.TrimSpace(line)) > 0 {
			// Final partial line: deliver it now, defer the EOF to the
			// next call so Serve processes it before terminating.
			return line, nil
		}
		return line, err
	}
}

// drainLine consumes the remainder of an oversized line so the stream
// is resynchronized at the next newline. It always terminates: the
// underlying reader either reaches the newline or EOF.
func drainLine(br *bufio.Reader) {
	for {
		_, err := br.ReadSlice('\n')
		if err == nil || err != bufio.ErrBufferFull {
			return
		}
	}
}
