// This standalone host module compares transports around the same pinned VM.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

type trace struct {
	Name      string
	Config    json.RawMessage
	Requests  []json.RawMessage
	Responses []json.RawMessage
}

func loadTrace(path string) (trace, error) {
	f, e := os.Open(path)
	if e != nil {
		return trace{}, e
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 4096), 16*1024*1024)
	t := trace{Name: filepath.Base(path)}
	if !s.Scan() {
		return t, fmt.Errorf("missing trace header: %s", path)
	}
	var header struct {
		Schema int
		Config json.RawMessage
	}
	if e = json.Unmarshal(s.Bytes(), &header); e != nil {
		return t, e
	}
	if header.Schema != 1 {
		return t, fmt.Errorf("unsupported trace schema")
	}
	t.Config = header.Config
	for s.Scan() {
		var row struct{ Request, Response json.RawMessage }
		if e = json.Unmarshal(s.Bytes(), &row); e != nil {
			return t, e
		}
		t.Requests = append(t.Requests, row.Request)
		t.Responses = append(t.Responses, row.Response)
	}
	if e = s.Err(); e != nil {
		return t, e
	}
	if len(t.Requests) == 0 {
		return t, fmt.Errorf("empty trace")
	}
	return t, nil
}

type client interface {
	request([]byte) ([]byte, error)
	close() error
}
type childClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	url    string
	http   http.Client
}

func startChild(ctx context.Context, runner, mode string) (*childClient, error) {
	arg := "--stdio"
	if mode == "http" {
		arg = "--http-session"
	}
	c := &childClient{cmd: exec.CommandContext(ctx, runner, arg), http: http.Client{Timeout: 20 * time.Second}}
	c.cmd.Stderr = os.Stderr
	var e error
	c.stdin, e = c.cmd.StdinPipe()
	if e != nil {
		return nil, e
	}
	stdout, e := c.cmd.StdoutPipe()
	if e != nil {
		return nil, e
	}
	c.stdout = bufio.NewReader(stdout)
	if e = c.cmd.Start(); e != nil {
		return nil, e
	}
	if mode == "http" {
		line, e := c.stdout.ReadBytes('\n')
		if e != nil {
			c.close()
			return nil, e
		}
		var h struct{ URL string }
		if e = json.Unmarshal(line, &h); e != nil {
			c.close()
			return nil, e
		}
		c.url = h.URL
	}
	return c, nil
}
func (c *childClient) request(input []byte) ([]byte, error) {
	if c.url != "" {
		r, e := c.http.Post(c.url, "application/json", bytes.NewReader(input))
		if e != nil {
			return nil, e
		}
		defer r.Body.Close()
		return io.ReadAll(r.Body)
	}
	if _, e := c.stdin.Write(append(input, '\n')); e != nil {
		return nil, e
	}
	return c.stdout.ReadBytes('\n')
}
func (c *childClient) close() error {
	c.http.CloseIdleConnections()
	c.stdin.Close()
	if c.url != "" {
		c.cmd.Process.Kill()
	}
	return c.cmd.Wait()
}

func decode(input []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(input))
	d.UseNumber()
	var value any
	e := d.Decode(&value)
	return value, e
}
func equalJSON(a, b []byte) bool {
	x, e := decode(a)
	if e != nil {
		return false
	}
	y, e := decode(b)
	return e == nil && reflect.DeepEqual(x, y)
}
func envelope(id int, op string, body any) []byte {
	message := map[string]any{"schema": 1, "id": id, "op": op}
	if op == "init" {
		message["config"] = body
	} else {
		message["calls"] = body
	}
	data, e := json.Marshal(message)
	if e != nil {
		panic(e)
	}
	return data
}
func unpack(reply []byte, id int) (json.RawMessage, error) {
	var response struct {
		Schema int
		ID     int
		Result json.RawMessage
		Error  json.RawMessage
	}
	if e := json.Unmarshal(reply, &response); e != nil {
		return nil, e
	}
	if response.Schema != 1 || response.ID != id {
		return nil, fmt.Errorf("response identity mismatch: %s", reply)
	}
	if len(response.Error) > 0 && string(response.Error) != "null" {
		return nil, fmt.Errorf("runner: %s", response.Error)
	}
	return response.Result, nil
}

type observation struct {
	Sample    int     `json:"sample"`
	Trace     string  `json:"trace"`
	Transport string  `json:"transport"`
	Batch     int     `json:"batch"`
	Calls     int     `json:"calls"`
	Startup   float64 `json:"startup_seconds"`
	Execution float64 `json:"fixture_seconds"`
	Total     float64 `json:"total_seconds"`
}

func replay(ctx context.Context, runner string, t trace, transport string, batch, sample int) (observation, error) {
	started := time.Now()
	row := observation{Sample: sample, Trace: t.Name, Transport: transport, Batch: batch, Calls: len(t.Requests)}
	var c client
	var e error
	if transport == "embedded" {
		c = newEmbedded()
	} else {
		c, e = startChild(ctx, runner, transport)
		if e != nil {
			return row, e
		}
	}
	defer c.close()
	reply, e := c.request(envelope(0, "init", t.Config))
	if e != nil {
		return row, e
	}
	result, e := unpack(reply, 0)
	if e != nil {
		return row, e
	}
	if !equalJSON(result, []byte(`{"ready":true}`)) {
		return row, fmt.Errorf("init did not become ready")
	}
	row.Startup = time.Since(started).Seconds()
	execute := time.Now()
	id := 0
	for offset := 0; offset < len(t.Requests); offset += batch {
		end := offset + batch
		if end > len(t.Requests) {
			end = len(t.Requests)
		}
		id++
		reply, e = c.request(envelope(id, "batch", t.Requests[offset:end]))
		if e != nil {
			return row, e
		}
		result, e = unpack(reply, id)
		if e != nil {
			return row, e
		}
		var responses []json.RawMessage
		if e = json.Unmarshal(result, &responses); e != nil {
			return row, e
		}
		if len(responses) != end-offset {
			return row, fmt.Errorf("batch response count mismatch")
		}
		for i, response := range responses {
			if !equalJSON(response, t.Responses[offset+i]) {
				return row, fmt.Errorf("%s %s batch %d request %d mismatch\ngot: %s\nwant: %s", t.Name, transport, batch, offset+i, response, t.Responses[offset+i])
			}
		}
	}
	row.Execution = time.Since(execute).Seconds()
	row.Total = time.Since(started).Seconds()
	return row, nil
}
func run() error {
	traceDir := flag.String("traces", "", "directory of captured traces")
	runner := flag.String("runner", "", "sidecar binary")
	library := flag.String("library", "", "same-core embedded dynamic library")
	output := flag.String("output", "", "new result file")
	samples := flag.Int("samples", 7, "repetitions")
	flag.Parse()
	if *samples < 1 || *traceDir == "" || *runner == "" || *library == "" || *output == "" {
		return fmt.Errorf("require traces, runner, library, output, samples >= 1")
	}
	if e := openLibrary(*library); e != nil {
		return e
	}
	paths, e := filepath.Glob(filepath.Join(*traceDir, "*.jsonl"))
	if e != nil {
		return e
	}
	sort.Strings(paths)
	if len(paths) != 4 {
		return fmt.Errorf("require token and three bounded traces; found %d", len(paths))
	}
	traces := []trace{}
	for _, path := range paths {
		t, e := loadTrace(path)
		if e != nil {
			return e
		}
		traces = append(traces, t)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	rows := []observation{}
	type choice struct {
		transport string
		batch     int
	}
	choices := []choice{{"http", 1}, {"http", 64}, {"stdio", 1}, {"stdio", 64}, {"embedded", 1}, {"embedded", 64}}
	rng := rand.New(rand.NewSource(20261005))
	for sample := 0; sample < *samples; sample++ {
		rng.Shuffle(len(choices), func(i, j int) { choices[i], choices[j] = choices[j], choices[i] })
		for _, mode := range choices {
			for _, t := range traces {
				row, e := replay(ctx, *runner, t, mode.transport, mode.batch, sample)
				if e != nil {
					return e
				}
				rows = append(rows, row)
			}
		}
		fmt.Fprintf(os.Stderr, "PASS sample %d/%d: all response bytes/state/CU/error/log assertions agree\n", sample+1, *samples)
	}
	data, e := json.MarshalIndent(map[string]any{"schema": 1, "equivalence_passed": true, "samples": *samples, "timings": rows,
		"scope": "same pinned LiteSVM core; full recorded RPC responses; init plus fixtures; shutdown excluded; embedded library dlopen excluded"}, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if e != nil {
		return e
	}
	defer f.Close()
	_, e = f.Write(append(data, '\n'))
	return e
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, strings.TrimSpace(e.Error()))
		os.Exit(1)
	}
}
