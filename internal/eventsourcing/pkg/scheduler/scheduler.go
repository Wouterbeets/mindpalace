package scheduler

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/la-clinique-e-sante/monorepo/back/domains/eventsourcing/pkg/model"
)

// Scheduler defines the interface for scheduling tasks.
type Scheduler interface {
	Schedule(at time.Time, task func() error)
	ListScheduledTasks() []model.ScheduledTask
	GetTaskStatus(at time.Time) string
}

// TaskScheduler is a concrete implementation of Scheduler.
type TaskScheduler struct {
	ctx          context.Context
	cancelFunc   context.CancelFunc
	tasks        sync.WaitGroup
	taskMutex    sync.Mutex
	scheduledMap map[time.Time]*model.ScheduledTask
}

// NewTaskScheduler creates a new TaskScheduler with an optional context.
// If no context is provided, context.Background() is used by default.
func NewTaskScheduler(ctx context.Context) *TaskScheduler {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancelFunc := context.WithCancel(ctx)
	return &TaskScheduler{
		ctx:          ctx,
		cancelFunc:   cancelFunc,
		scheduledMap: make(map[time.Time]*model.ScheduledTask),
	}
}

// Schedule schedules a task to be executed at a specific time.
func (s *TaskScheduler) Schedule(at time.Time, task func() error) {
	delay := time.Until(at)
	if delay <= 0 {
		// If the time is in the past, run the task immediately in a non-blocking way.
		go task()
		return
	}

	s.tasks.Add(1)
	s.taskMutex.Lock()
	s.scheduledMap[at] = &model.ScheduledTask{
		ScheduledTime: at,
		TaskFunc:      task,
		Status:        "pending",
	}
	s.taskMutex.Unlock()

	go func() {
		defer s.tasks.Done()
		select {
		case <-s.ctx.Done():
			// If the context is cancelled, mark the task as cancelled.
			s.taskMutex.Lock()
			if taskInfo, exists := s.scheduledMap[at]; exists {
				taskInfo.Status = "cancelled"
			}
			s.taskMutex.Unlock()
			return
		case <-time.After(delay):
			// Time has elapsed, execute the task.
			if err := task(); err != nil {
				log.Printf("Error executing scheduled task: %v", err)
			}
			// Mark the task as completed.
			s.taskMutex.Lock()
			if taskInfo, exists := s.scheduledMap[at]; exists {
				taskInfo.Status = "completed"
			}
			s.taskMutex.Unlock()
		}
	}()
}

// Stop waits for all scheduled tasks to complete and cancels any pending tasks.
func (s *TaskScheduler) Stop() {
	s.cancelFunc() // Cancel the context to stop any pending tasks.
	s.tasks.Wait() // Wait for all tasks to complete.
}

// ListScheduledTasks returns a list of all scheduled tasks with their statuses.
func (s *TaskScheduler) ListScheduledTasks() []model.ScheduledTask {
	s.taskMutex.Lock()
	defer s.taskMutex.Unlock()

	tasks := make([]model.ScheduledTask, 0, len(s.scheduledMap))
	for _, taskInfo := range s.scheduledMap {
		tasks = append(tasks, *taskInfo)
	}
	return tasks
}

// GetTaskStatus returns the status of a task scheduled at the given time.
func (s *TaskScheduler) GetTaskStatus(at time.Time) string {
	s.taskMutex.Lock()
	defer s.taskMutex.Unlock()

	if taskInfo, exists := s.scheduledMap[at]; exists {
		return taskInfo.Status
	}
	return "not found"
}
