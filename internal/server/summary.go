package server

import (
	"cmp"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/genhoi/ai-threads/internal/agents"
	"github.com/genhoi/ai-threads/internal/model"
	"github.com/genhoi/ai-threads/internal/settings"
	"github.com/genhoi/ai-threads/internal/summary"
)

// Сводки по сессии и за несколько дней. Пишет их агент без человека в задании (пакет summary);
// ход уходит событиями job, готовая сводка за несколько дней — событием digest.

const (
	// candidatesAge и candidatesMax — сколько сессий того же проекта и не старше скольких
	// модель может назвать связанными.
	candidatesAge = 30 * 24 * time.Hour
	candidatesMax = 30
	// digestSessions — больше стольких недавних сессий сводка за период не берёт.
	digestSessions = 200
)

// handleSummary — POST /api/sessions/{key}/summary: составить сводку по сессии.
func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) error {
	key := r.PathValue("key")
	var req SummaryRequest
	if err := decode(r, &req, bodyLimit, true); err != nil {
		return err
	}
	sess, ok := s.session(key)
	if !ok {
		return notFound()
	}
	target := summary.Target{Kind: summary.KindSummary, Key: key}
	if j := s.jobs.Find(target); j != nil {
		writeJSON(w, http.StatusOK, JobStarted{Job: jobView(j.Status())})
		return nil
	}
	if !isFile(sess.Journal) {
		return apiErr(http.StatusConflict, "no_journal")
	}
	runner, err := s.pickRunner(summary.KindSummary, req.Agent)
	if err != nil {
		return err
	}
	c := s.viewCtx()
	in := summary.SessionInput{Session: sess, Source: c.agents[sess.Agent], Title: c.marks.Names[key]}
	// Связанными модель может назвать недавние сессии того же проекта, как в первой версии.
	if path := projectPath(sess); path != "" {
		for _, other := range s.catalog.Sessions() {
			if len(in.Candidates) == candidatesMax || time.Since(other.Updated) > candidatesAge {
				break
			}
			k := other.Key()
			if k == key || projectPath(other) != path || other.Empty || c.service(other) || c.marks.Marks(k).Hidden {
				continue
			}
			in.Candidates = append(in.Candidates, summary.Candidate{Key: k, Title: cmp.Or(c.marks.Names[k], other.Title)})
		}
	}
	run := s.summaryRun(summary.KindSummary, runner, r)
	job, _, err := s.jobs.Start(target, runner.ID(), func(ctx context.Context, j *summary.Job) error {
		sum, err := summary.Summarize(ctx, j, run, in)
		if err != nil {
			return err
		}
		return s.store.SetSummary(key, sum)
	})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, JobStarted{Job: jobView(job.Status())})
	return nil
}

// handleDigest — GET /api/digest?days=: сохранённая сводка за период и идущее задание.
func (s *Server) handleDigest(w http.ResponseWriter, r *http.Request) error {
	days, err := strconv.Atoi(r.URL.Query().Get("days"))
	if err != nil || !summary.ValidDays(days) {
		return apiErr(http.StatusBadRequest, "bad_days")
	}
	view := DigestView{Digest: s.digest(days, s.lang(r))}
	if j := s.jobs.Find(summary.Target{Kind: summary.KindDigest, Days: days}); j != nil {
		v := jobView(j.Status())
		view.Job = &v
	}
	writeJSON(w, http.StatusOK, view)
	return nil
}

// handleStartDigest — POST /api/digest: составить сводку за 3, 5 или 7 дней по сессиям
// без служебных, скрытых и пустых. Прежняя сводка остаётся, пока новая не готова.
func (s *Server) handleStartDigest(w http.ResponseWriter, r *http.Request) error {
	var req DigestRequest
	if err := decode(r, &req, bodyLimit, false); err != nil {
		return err
	}
	if !summary.ValidDays(req.Days) {
		return apiErr(http.StatusBadRequest, "bad_days")
	}
	target := summary.Target{Kind: summary.KindDigest, Days: req.Days}
	if j := s.jobs.Find(target); j != nil {
		writeJSON(w, http.StatusOK, JobStarted{Job: jobView(j.Status())})
		return nil
	}
	runner, err := s.pickRunner(summary.KindDigest, req.Agent)
	if err != nil {
		return err
	}
	now := time.Now()
	c := s.viewCtx()
	in := summary.DigestInput{Days: req.Days, Now: now}
	for _, sess := range s.catalog.Sessions() {
		if !summary.InPeriod(sess, req.Days, now) {
			break // сначала недавние
		}
		key := sess.Key()
		m := c.marks.Marks(key)
		if sess.Empty || sess.Journal == "" || m.Hidden || c.service(sess) {
			continue
		}
		state := model.StateIdle
		if l, ok := c.live[key]; ok {
			state = l.State
		} else if st, ok := c.registry[key]; ok {
			state = st.State
		}
		in.Sessions = append(in.Sessions, summary.DigestSession{Session: sess, Source: c.agents[sess.Agent],
			Title: m.Name, Path: projectPath(sess), Summary: m.Summary, Done: m.Done, State: state})
		if len(in.Sessions) == digestSessions {
			break
		}
	}
	if len(in.Sessions) == 0 {
		return apiErr(http.StatusConflict, "no_period_sessions")
	}
	run := s.summaryRun(summary.KindDigest, runner, r)
	job, _, err := s.jobs.Start(target, runner.ID(), func(ctx context.Context, j *summary.Job) error {
		d, err := summary.MakeDigest(ctx, j, run, in)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(d)
		if err != nil {
			return err
		}
		return s.store.SetDigest(req.Days, raw)
	})
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, JobStarted{Job: jobView(job.Status())})
	return nil
}

// handleJobs — GET /api/jobs: идущие задания, для страницы, открытой посреди работы.
func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) error {
	list := s.jobs.Running()
	slices.SortFunc(list, func(a, b summary.Status) int { return a.Started.Compare(b.Started) })
	out := make([]JobView, 0, len(list))
	for _, st := range list {
		out = append(out, jobView(st))
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// handleCancelJob — POST /api/jobs/{id}/cancel: остановить агента. Прежняя сводка остаётся.
func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, http.StatusOK, CancelResult{Cancelled: s.jobs.Cancel(r.PathValue("id"))})
	return nil
}

// pickRunner — кто пишет сводку: агент из запроса или из настроек. «Авто» — первый
// установленный в порядке Kimi, Claude Code, Codex, Grok, который сейчас не занят другой
// сводкой; если заняты все — первый установленный.
func (s *Server) pickRunner(kind, want string) (agents.Agent, error) {
	if want == "" {
		set := s.settings.Get()
		want = set.Summary.Agent
		if kind == summary.KindDigest {
			want = set.Digest.Agent
		}
	}
	if want != "auto" {
		if !slices.Contains(settings.Runners, want) {
			return nil, apiErr(http.StatusBadRequest, "not_runner", "value", want)
		}
		a := s.agent(want)
		if err := s.checkAgent(a, want); err != nil {
			return nil, err
		}
		return a, nil
	}
	var first agents.Agent
	var names []string
	for _, id := range settings.Runners {
		a := s.agent(id)
		if a == nil {
			continue
		}
		names = append(names, a.Info().Name)
		if !a.Enabled() || a.Program() == "" {
			continue
		}
		if !s.jobs.Busy(id) {
			return a, nil
		}
		if first == nil {
			first = a
		}
	}
	if first != nil {
		return first, nil
	}
	return nil, apiErr(http.StatusConflict, "no_runner", "value", strings.Join(names, ", "))
}

// summaryRun — как запускать агента: предел времени из настроек, язык ответа — язык
// интерфейса, рабочие папки — в папке данных рядом с state.json.
func (s *Server) summaryRun(kind string, a agents.Agent, r *http.Request) summary.Run {
	return summary.Run{Runner: a, Timeout: s.settings.Get().Timeout(kind), Lang: s.lang(r),
		Dir: filepath.Join(filepath.Dir(s.store.Path()), "runs")}
}

// jobChanged рассылает ход задания всем открытым страницам.
func (s *Server) jobChanged(st summary.Status) {
	s.events.broadcast(frame("job", jobView(st)))
}

// digestChanged рассылает сохранённую сводку за период; key — число дней.
func (s *Server) digestChanged(key string) {
	days, err := strconv.Atoi(key)
	if err != nil {
		return
	}
	if d := s.digest(days, s.lang(nil)); d != nil {
		s.events.broadcast(frame("digest", d))
	}
}

// digest — сохранённая сводка за days дней; nil, если её нет или она не читается.
func (s *Server) digest(days int, lang string) *summary.Digest {
	raw, err := s.store.Digest(days)
	if err != nil {
		return nil
	}
	d, err := summary.Decode(raw, days, lang, func(key string) string {
		if sess, ok := s.catalog.Get(key); ok {
			return projectPath(sess)
		}
		return ""
	})
	if err != nil {
		return nil
	}
	return d
}

func jobView(st summary.Status) JobView {
	return JobView{ID: st.ID, Kind: st.Target.Kind, Key: st.Target.Key, Days: st.Target.Days, Agent: st.Agent,
		Stage: st.Stage, Reads: st.Reads, Started: st.Started.Unix(), Elapsed: int(st.Elapsed / time.Second),
		Done: st.Done, Error: st.Error, Output: st.Output}
}

func isFile(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
