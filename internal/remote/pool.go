package remote

import (
	"context"
	"io"
	"sync"
)

// poolSize is how many long-lived workers one Windows target keeps. It is
// not a setting. One command, including a long-running inotifywait, holds
// one worker. Further commands wait.
const poolSize = 4

// workerPipe is one worker process: frames go in through in and come
// back through out. kill ends the process.
type workerPipe struct {
	in   io.WriteCloser
	out  io.ReadCloser
	kill func()
	// done is closed when the process has exited.
	done <-chan struct{}
}

// pool runs commands on a fixed set of workers. Cancel kills that
// worker and starts another. Stop ends the queue and every worker.
type pool struct {
	open   func() (workerPipe, error)
	onLost func(error)

	mu      sync.Mutex
	cond    *sync.Cond
	size    int
	live    int
	busy    int
	closed  bool
	lost    bool
	workers []*worker
	queue   []*poolJob
}

type worker struct {
	pipe workerPipe
	f    *framer
	kill func()
	dead bool
}

type poolJob struct {
	ctx            context.Context
	script         string
	stdin          io.Reader
	stdout, stderr io.Writer
	code           int
	err            error
	done           chan struct{}
	once           sync.Once
}

func newPool(open func() (workerPipe, error), onLost func(error)) *pool {
	p := &pool{open: open, onLost: onLost, size: poolSize}
	p.cond = sync.NewCond(&p.mu)
	return p
}

func (p *pool) Start() error {
	for range p.size {
		if err := p.add(); err != nil {
			p.Stop()
			return err
		}
	}
	return nil
}

func (p *pool) add() error {
	pipe, err := p.open()
	if err != nil {
		return err
	}
	w := &worker{
		pipe: pipe,
		f:    newFramer(pipe.out, pipe.in),
		kill: pipe.kill,
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		w.kill()
		return ErrOffline
	}
	p.workers = append(p.workers, w)
	p.live++
	p.cond.Broadcast()
	p.mu.Unlock()
	go func() {
		if pipe.done != nil {
			<-pipe.done
		}
		p.mu.Lock()
		w.dead = true
		p.cond.Broadcast()
		p.mu.Unlock()
	}()
	go p.loop(w)
	return nil
}

func (p *pool) loop(w *worker) {
	defer p.workerGone(w)
	for {
		job := p.take(w)
		if job == nil {
			return
		}
		p.addBusy(1)
		stop := make(chan struct{})
		go func() {
			select {
			case <-job.ctx.Done():
				w.kill()
			case <-stop:
			}
		}()
		code, err := w.f.roundTrip(job.ctx, job.script, job.stdin, job.stdout, job.stderr)
		close(stop)
		if job.ctx.Err() != nil {
			p.addBusy(-1)
			w.kill()
			job.finish(code, job.ctx.Err())
			return
		}
		if err != nil {
			p.addBusy(-1)
			w.kill()
			job.finish(0, p.closedErr(err))
			return
		}
		p.addBusy(-1)
		job.finish(code, nil)
	}
}

func (p *pool) addBusy(n int) {
	p.mu.Lock()
	p.busy += n
	p.mu.Unlock()
}

func (p *pool) take(w *worker) *poolJob {
	p.mu.Lock()
	defer p.mu.Unlock()
	for len(p.queue) == 0 && !p.closed && !w.dead {
		p.cond.Wait()
	}
	if p.closed || w.dead || len(p.queue) == 0 {
		return nil
	}
	job := p.queue[0]
	p.queue = p.queue[1:]
	return job
}

// Call runs script on one worker. It waits when every worker is busy.
func (p *pool) Call(ctx context.Context, script string, stdin io.Reader, stdout, stderr io.Writer) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	job := &poolJob{
		ctx: ctx, script: script, stdin: stdin, stdout: stdout, stderr: stderr,
		done: make(chan struct{}),
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		if p.lost {
			return 0, ErrLost
		}
		return 0, ErrOffline
	}
	p.queue = append(p.queue, job)
	p.cond.Signal()
	p.mu.Unlock()
	select {
	case <-ctx.Done():
		if p.dropQueued(job) {
			return 0, ctx.Err()
		}
		<-job.done
		return job.code, job.err
	case <-job.done:
		return job.code, job.err
	}
}

func (p *pool) dropQueued(job *poolJob) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, q := range p.queue {
		if q == job {
			p.queue = append(p.queue[:i], p.queue[i+1:]...)
			return true
		}
	}
	return false
}

func (j *poolJob) finish(code int, err error) {
	j.once.Do(func() {
		j.code, j.err = code, err
		close(j.done)
	})
}

func (p *pool) workerGone(w *worker) {
	p.mu.Lock()
	p.live--
	for i, cur := range p.workers {
		if cur == w {
			p.workers = append(p.workers[:i], p.workers[i+1:]...)
			break
		}
	}
	closed := p.closed
	p.mu.Unlock()
	if w.kill != nil {
		w.kill()
	}
	if closed {
		return
	}
	if err := p.add(); err != nil {
		p.failIfEmpty()
	}
}

// failIfEmpty marks the pool lost once no worker is left. It reports
// ErrLost rather than why the last worker failed to start.
func (p *pool) failIfEmpty() {
	p.mu.Lock()
	if p.closed || p.live > 0 {
		p.mu.Unlock()
		return
	}
	p.closed, p.lost = true, true
	jobs := p.queue
	p.queue = nil
	p.cond.Broadcast()
	p.mu.Unlock()
	for _, job := range jobs {
		job.finish(0, ErrLost)
	}
	if p.onLost != nil {
		p.onLost(ErrLost)
	}
}

func (p *pool) closedErr(err error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.lost {
		return ErrLost
	}
	if p.closed {
		return ErrOffline
	}
	return err
}

// Stop ends queued commands and every worker. It is what Disconnect does
// to a session pool.
func (p *pool) Stop() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	jobs := p.queue
	p.queue = nil
	workers := append([]*worker(nil), p.workers...)
	p.cond.Broadcast()
	p.mu.Unlock()
	for _, job := range jobs {
		job.finish(0, ErrOffline)
	}
	for _, w := range workers {
		if w.kill != nil {
			w.kill()
		}
	}
}

func (p *pool) counts() (live, busy, queued int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.live, p.busy, len(p.queue)
}
