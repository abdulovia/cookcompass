package app

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/abdulovia/cookcompass/internal/recipes"
)

// LongRunning is implemented by recipe sources that take too long for a
// request (e.g. a full TheMealDB pass); the admin runs them as jobs.
type LongRunning interface{ LongRunning() bool }

// JobStatus describes the current or last background run of one source.
type JobStatus struct {
	Source   string
	Running  bool
	Started  time.Time
	Finished time.Time
	Summary  string // ingest result counts
	Err      string
}

// Jobs runs at most one background scrape per source and remembers the
// last outcome for the admin queue page. State is in memory: a restart
// forgets it, and the queue itself is the durable result.
type Jobs struct {
	mu    sync.Mutex
	state map[string]*JobStatus
	wg    sync.WaitGroup
}

// jobTimeout bounds one background run.
const jobTimeout = 20 * time.Minute

// StartScrape runs a recipe source in the background. It returns false when
// that source is already running.
func (a *App) StartScrape(source string) bool {
	j := &a.jobs
	j.mu.Lock()
	if j.state == nil {
		j.state = map[string]*JobStatus{}
	}
	if st, ok := j.state[source]; ok && st.Running {
		j.mu.Unlock()
		return false
	}
	st := &JobStatus{Source: source, Running: true, Started: time.Now()}
	j.state[source] = st
	j.wg.Add(1)
	j.mu.Unlock()

	go func() {
		defer j.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
		defer cancel()
		res, err := a.ScrapeRecipes(ctx, source, recipes.FetchOpts{})
		a.Log.Info("background scrape finished", "source", source, "result", res.String(), "err", err)
		j.mu.Lock()
		defer j.mu.Unlock()
		st.Running, st.Finished, st.Summary = false, time.Now(), res.String()
		if err != nil {
			st.Err = err.Error()
		}
	}()
	return true
}

// JobStatuses lists known jobs, running first, then by source name.
func (a *App) JobStatuses() []JobStatus {
	j := &a.jobs
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]JobStatus, 0, len(j.state))
	for _, st := range j.state {
		out = append(out, *st)
	}
	sort.Slice(out, func(i, k int) bool {
		if out[i].Running != out[k].Running {
			return out[i].Running
		}
		return out[i].Source < out[k].Source
	})
	return out
}

// WaitJobs blocks until every background job has finished (tests, shutdown).
func (a *App) WaitJobs() { a.jobs.wg.Wait() }

// IsLongRunning reports whether a source should run as a background job.
func (a *App) IsLongRunning(source string) bool {
	srcs, err := a.RecipeSources()
	if err != nil {
		return false
	}
	lr, ok := srcs[source].(LongRunning)
	return ok && lr.LongRunning()
}
