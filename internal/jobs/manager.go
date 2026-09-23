package jobs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

type State string

const (
	Queued    State = "queued"
	Running   State = "running"
	Done      State = "done"
	Failed    State = "failed"
	Cancelled State = "cancelled"
)

type Snapshot struct {
	ID         string     `json:"id"`
	Type       string     `json:"type"`
	State      State      `json:"state"`
	Rows       int64      `json:"rows"`
	Bytes      int64      `json:"bytes"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Error      string     `json:"error,omitempty"`
}

type Work func(context.Context, *Progress) error

type Manager struct {
	mu            sync.Mutex
	jobs          map[string]*entry
	active        map[string]int
	maxPerSession int
	tempDir       string
	closed        bool
	wg            sync.WaitGroup
}

type entry struct {
	id         string
	session    string
	typeName   string
	mu         sync.Mutex
	state      State
	rows       atomic.Int64
	bytes      atomic.Int64
	startedAt  *time.Time
	finishedAt *time.Time
	errText    string
	cancel     context.CancelFunc
	cancelled  atomic.Bool
	done       chan struct{}
	tempFiles  []string
}

type Progress struct {
	manager *Manager
	job     *entry
}

func New(tempDir string, maxPerSession int) *Manager {
	if maxPerSession < 1 {
		maxPerSession = 1
	}
	if tempDir == "" {
		tempDir = filepath.Join(os.TempDir(), "pglight-jobs")
	}
	return &Manager{jobs: make(map[string]*entry), active: make(map[string]int), maxPerSession: maxPerSession, tempDir: tempDir}
}

func (m *Manager) Start(sessionID, typeName string, work Work) (Snapshot, error) {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(typeName) == "" || work == nil {
		return Snapshot{}, fmt.Errorf("session, type and work are required")
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return Snapshot{}, fmt.Errorf("job manager is closed")
	}
	if m.active[sessionID] >= m.maxPerSession {
		m.mu.Unlock()
		return Snapshot{}, fmt.Errorf("too many active jobs for this session")
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &entry{id: uuid.NewString(), session: sessionID, typeName: typeName, state: Queued, cancel: cancel, done: make(chan struct{})}
	m.jobs[job.id] = job
	m.active[sessionID]++
	m.wg.Add(1)
	m.mu.Unlock()
	go m.run(ctx, job, work)
	return job.snapshot(), nil
}

func (m *Manager) run(ctx context.Context, job *entry, work Work) {
	defer m.wg.Done()
	started := time.Now()
	job.mu.Lock()
	job.state, job.startedAt = Running, &started
	job.mu.Unlock()
	err := work(ctx, &Progress{manager: m, job: job})
	finished := time.Now()
	job.mu.Lock()
	switch {
	case job.cancelled.Load() || errors.Is(ctx.Err(), context.Canceled):
		job.state = Cancelled
	case err != nil:
		job.state = Failed
		job.errText = err.Error()
		if len(job.errText) > 4096 {
			job.errText = job.errText[:4096]
		}
	default:
		job.state = Done
	}
	job.finishedAt = &finished
	state := job.state
	tempFiles := append([]string(nil), job.tempFiles...)
	job.mu.Unlock()
	if state != Done {
		for _, path := range tempFiles {
			_ = os.Remove(path)
		}
	}
	close(job.done)
	m.mu.Lock()
	if m.active[job.session] > 0 {
		m.active[job.session]--
	}
	m.mu.Unlock()
}

func (m *Manager) Get(sessionID, id string) (Snapshot, error) {
	job, err := m.lookup(sessionID, id)
	if err != nil {
		return Snapshot{}, err
	}
	return job.snapshot(), nil
}

func (m *Manager) Cancel(sessionID, id string) error {
	job, err := m.lookup(sessionID, id)
	if err != nil {
		return err
	}
	job.mu.Lock()
	if job.state == Done || job.state == Failed || job.state == Cancelled {
		job.mu.Unlock()
		return fmt.Errorf("job is already finished")
	}
	job.cancelled.Store(true)
	job.mu.Unlock()
	job.cancel()
	return nil
}

func (m *Manager) Wait(ctx context.Context, sessionID, id string) (Snapshot, error) {
	job, err := m.lookup(sessionID, id)
	if err != nil {
		return Snapshot{}, err
	}
	select {
	case <-ctx.Done():
		return Snapshot{}, ctx.Err()
	case <-job.done:
		return job.snapshot(), nil
	}
}

func (m *Manager) lookup(sessionID, id string) (*entry, error) {
	m.mu.Lock()
	job := m.jobs[id]
	m.mu.Unlock()
	if job == nil || job.session != sessionID {
		return nil, fmt.Errorf("job not found")
	}
	return job, nil
}

func (j *entry) snapshot() Snapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	return Snapshot{ID: j.id, Type: j.typeName, State: j.state, Rows: j.rows.Load(), Bytes: j.bytes.Load(), StartedAt: j.startedAt, FinishedAt: j.finishedAt, Error: j.errText}
}

func (p *Progress) AddRows(n int64) {
	if n > 0 {
		p.job.rows.Add(n)
	}
}
func (p *Progress) AddBytes(n int64) {
	if n > 0 {
		p.job.bytes.Add(n)
	}
}

func (p *Progress) CreateTemp(suffix string) (*os.File, error) {
	if suffix != "" && (filepath.Base(suffix) != suffix || strings.ContainsAny(suffix, `/\\`)) {
		return nil, fmt.Errorf("invalid temporary-file suffix")
	}
	if err := os.MkdirAll(p.manager.tempDir, 0700); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(p.manager.tempDir, p.job.id+"-*"+suffix)
	if err != nil {
		return nil, err
	}
	p.job.mu.Lock()
	p.job.tempFiles = append(p.job.tempFiles, f.Name())
	p.job.mu.Unlock()
	return f, nil
}

func (m *Manager) RemoveTempFiles(sessionID, id string) error {
	job, err := m.lookup(sessionID, id)
	if err != nil {
		return err
	}
	job.mu.Lock()
	files := append([]string(nil), job.tempFiles...)
	job.tempFiles = nil
	job.mu.Unlock()
	var removeErr error
	for _, path := range files {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) && removeErr == nil {
			removeErr = err
		}
	}
	return removeErr
}

func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	jobs := make([]*entry, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, job)
	}
	m.mu.Unlock()
	for _, job := range jobs {
		select {
		case <-job.done:
		default:
			job.cancelled.Store(true)
			job.cancel()
		}
	}
	m.wg.Wait()
	for _, job := range jobs {
		_ = m.RemoveTempFiles(job.session, job.id)
	}
}
