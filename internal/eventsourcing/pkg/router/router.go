package router

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/la-clinique-e-sante/monorepo/back/domains/eventsourcing/pkg/eventsourcing"
	"github.com/la-clinique-e-sante/monorepo/back/domains/eventsourcing/pkg/model"
	"github.com/la-clinique-e-sante/monorepo/back/domains/login/pkg/middleware"
)

func NewDefault() func(r chi.Router) {
	es := eventsourcing.NewInMemoryEventStore()
	source := eventsourcing.NewWithDefaultLock(es)
	return New(source.Scheduler)
}

type SchedulerReader interface {
	ListScheduledTasks() []model.ScheduledTask
	GetTaskStatus(at time.Time) string
}

func New(sch SchedulerReader) func(r chi.Router) {
	return func(r chi.Router) {
		r.With(middleware.NewDefaultBasicAuthMiddleware()).Get("/scheduled", NewGetScheduledTasksHandler(sch))
	}
}

func NewGetScheduledTasksHandler(sch SchedulerReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Retrieve the list of scheduled tasks
		tasks := sch.ListScheduledTasks()

		// Encode the list of tasks to JSON
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(tasks); err != nil {
			http.Error(w, "Failed to encode scheduled tasks", http.StatusInternalServerError)
			return
		}
	}
}
