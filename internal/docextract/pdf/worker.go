package pdf

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/docextract"
	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"github.com/CristhianRudolf7/excalibur-agent/internal/safego"
)

type request struct {
	Path      string
	Operation string
	Page      int
	DPI       int
	MaxPages  int
}
type response struct {
	Document docextract.Document
	Page     docextract.Page
	PNG      []byte
	Error    string
	Code     string
}
type session interface {
	call(context.Context, request) (response, error)
	close()
}
type localSession struct {
	root      context.Context
	engine    *engine
	file      *os.File
	path      string
	engineCtx context.Context
	cancel    context.CancelFunc
}

func (s *localSession) close() {
	if s.engine != nil {
		s.engine.close()
		s.engine = nil
	}
	if s.file != nil {
		_ = s.file.Close()
		s.file = nil
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
}
func (s *localSession) call(ctx context.Context, q request) (response, error) {
	if err := ctx.Err(); err != nil {
		return response{}, err
	}
	if s.engine == nil || s.path != q.Path {
		s.close()
		s.path = q.Path
		s.engineCtx, s.cancel = context.WithCancel(s.root)
	}
	type reply struct {
		result response
		err    error
	}
	done := make(chan reply, 1)
	safego.Go("pdf-in-process", func(_ string, _ any, _ []byte) { done <- reply{err: errors.New("pdf: engine panic")} }, func() {
		r, err := s.execute(q)
		done <- reply{r, err}
	})
	select {
	case r := <-done:
		if r.err != nil {
			s.close()
		}
		return r.result, r.err
	case <-ctx.Done():
		s.cancel()
		<-done
		s.close()
		return response{}, ctx.Err()
	}
}
func (s *localSession) execute(q request) (response, error) {
	var result response
	if s.engine == nil {
		f, err := os.Open(q.Path)
		if err != nil {
			return result, err
		}
		s.file = f
		info, err := f.Stat()
		if err != nil {
			return result, err
		}
		s.engine, err = openEngine(s.engineCtx, f, info.Size())
		if err != nil {
			return result, err
		}
	}
	var err error
	switch q.Operation {
	case "metadata":
		if q.MaxPages <= 0 {
			q.MaxPages = 2000
		}
		result.Document, err = s.engine.metadata(q.MaxPages)
	case "page":
		result.Page, err = s.engine.page(q.Page)
	case "render":
		if q.Page < 1 || q.DPI < 72 || q.DPI > 300 {
			return result, errors.New("pdf: invalid render request")
		}
		result.PNG, err = s.engine.render(q.Page, q.DPI)
	default:
		err = errors.New("pdf: unknown worker operation")
	}
	return result, err
}

// ServeWorker serves the private JSON-lines protocol until EOF. Stdout must be
// reserved for protocol responses. The binary calls this only for its hidden
// internal pdf-worker command. A document reuses one Wasm instance. A failed request discards it before
// the next page; compiled Wasm is cached across workers.
func ServeWorker(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	enc := json.NewEncoder(out)
	local := &localSession{root: ctx}
	defer local.close()
	for scanner.Scan() {
		var q request
		if err := json.Unmarshal(scanner.Bytes(), &q); err != nil {
			return err
		}
		timeout := 30 * time.Second
		if q.Operation == "metadata" {
			timeout = 10 * time.Minute
		}
		pageCtx, cancel := context.WithTimeout(ctx, timeout)
		r, err := local.call(pageCtx, q)
		cancel()
		if err != nil {
			r = response{Error: err.Error()}
			switch {
			case errors.Is(err, ErrEncrypted):
				r.Code = "encrypted"
			case errors.Is(err, ErrTooLarge):
				r.Code = "too_large"
			}
		}
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return scanner.Err()
}

type worker struct {
	options Options
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	group   platform.ProcessGroup
	sampler platform.ResourceSampler
	wait    chan error
}

func newWorker(opts Options) (*worker, error) {
	g, s := workerPlatform()
	if opts.ProcessGroup != nil {
		g = opts.ProcessGroup
	}
	if opts.ResourceSampler != nil {
		s = opts.ResourceSampler
	}
	if g == nil || s == nil {
		return nil, ErrUnsupported
	}
	return &worker{options: opts, group: g, sampler: s}, nil
}
func (w *worker) start() error {
	path := w.options.WorkerPath
	if path == "" {
		var err error
		path, err = os.Executable()
		if err != nil {
			return err
		}
	}
	cmd := exec.Command(path, "internal", "pdf-worker")
	w.group.Configure(cmd)
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		_ = in.Close()
		return err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		_ = in.Close()
		_ = out.Close()
		return err
	}
	w.cmd, w.stdin, w.stdout = cmd, in, out
	w.wait = make(chan error, 1)
	wait := w.wait
	safego.Go("pdf-worker-wait", nil, func() { wait <- cmd.Wait() })
	return nil
}
func (w *worker) close() {
	if w.cmd == nil {
		return
	}
	_ = w.stdin.Close()
	_ = w.group.Kill(w.cmd.Process.Pid)
	<-w.wait
	_ = w.stdout.Close()
	w.cmd = nil
}
func (w *worker) call(ctx context.Context, q request) (response, error) {
	var zero response
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if w.cmd == nil {
		if err := w.start(); err != nil {
			return zero, err
		}
	}
	type reply struct {
		result response
		err    error
	}
	done := make(chan reply, 1)
	in, out := w.stdin, w.stdout
	safego.Go("pdf-worker-request", func(_ string, _ any, _ []byte) { done <- reply{err: errors.New("pdf: worker protocol panic")} }, func() {
		var r response
		err := json.NewEncoder(in).Encode(q)
		if err == nil {
			err = json.NewDecoder(io.LimitReader(out, 128<<20)).Decode(&r)
		}
		done <- reply{r, err}
	})
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	limit := w.options.MemoryLimit
	if limit == 0 {
		limit = 1 << 30
	}
	stop := func(err error) (response, error) { w.close(); <-done; return zero, err }
	for {
		select {
		case <-ctx.Done():
			return stop(ctx.Err())
		case <-ticker.C:
			if sample, err := w.sampler.Sample(w.cmd.Process.Pid); err == nil && sample.RSSBytes > limit {
				return stop(errors.New("pdf: worker memory limit exceeded"))
			}
		case reply := <-done:
			if reply.err != nil {
				w.close()
				return zero, fmt.Errorf("pdf: worker exited: %w", reply.err)
			}
			if reply.result.Error != "" {
				switch reply.result.Code {
				case "encrypted":
					return zero, ErrEncrypted
				case "too_large":
					return zero, ErrTooLarge
				}
				return zero, errors.New(reply.result.Error)
			}
			return reply.result, nil
		}
	}
}
