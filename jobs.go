package main

import (
	"fmt"
	"strconv"
	"sync"
	"time"
)

// Job tracks a long-running task so the UI can poll its progress.
type Job struct {
	mu       sync.Mutex
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Status   string    `json:"status"` // running, done, error
	Step     string    `json:"step"`
	Progress float64   `json:"progress"` // 0..1, -1 = unknown
	Log      []string  `json:"log"`
	Error    string    `json:"error,omitempty"`
	Result   any       `json:"result,omitempty"`
	Started  time.Time `json:"started"`
}

var (
	jobsMu sync.Mutex
	jobs   = map[string]*Job{}
	jobSeq int
)

func startJob(title string, fn func(j *Job) (any, error)) *Job {
	jobsMu.Lock()
	jobSeq++
	j := &Job{ID: strconv.Itoa(jobSeq), Title: title, Status: "running", Progress: -1, Started: time.Now()}
	jobs[j.ID] = j
	jobsMu.Unlock()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				j.fail(fmt.Errorf("interner Fehler: %v", r))
			}
		}()
		res, err := fn(j)
		if err != nil {
			j.fail(err)
			return
		}
		j.mu.Lock()
		j.Status = "done"
		j.Progress = 1
		j.Result = res
		j.Log = append(j.Log, "Fertig.")
		j.mu.Unlock()
	}()
	return j
}

func (j *Job) fail(err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Status = "error"
	j.Error = err.Error()
	j.Log = append(j.Log, "Fehler: "+err.Error())
	logf("job %s failed: %v", j.Title, err)
}

func (j *Job) logf(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	j.mu.Lock()
	j.Log = append(j.Log, msg)
	if len(j.Log) > 500 {
		j.Log = j.Log[len(j.Log)-500:]
	}
	j.mu.Unlock()
	logf("[%s] %s", j.Title, msg)
}

func (j *Job) setStep(step string, progress float64) {
	j.mu.Lock()
	j.Step = step
	j.Progress = progress
	j.mu.Unlock()
}

func (j *Job) setProgress(p float64) {
	j.mu.Lock()
	j.Progress = p
	j.mu.Unlock()
}

func (j *Job) snapshot() map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	return map[string]any{
		"id": j.ID, "title": j.Title, "status": j.Status, "step": j.Step,
		"progress": j.Progress, "log": append([]string(nil), j.Log...),
		"error": j.Error, "result": j.Result,
	}
}

func getJob(id string) *Job {
	jobsMu.Lock()
	defer jobsMu.Unlock()
	return jobs[id]
}

func anyJobRunning() bool {
	jobsMu.Lock()
	defer jobsMu.Unlock()
	for _, j := range jobs {
		j.mu.Lock()
		r := j.Status == "running"
		j.mu.Unlock()
		if r {
			return true
		}
	}
	return false
}
