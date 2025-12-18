package worker

import (
	"sync"


	// "gopkg.in/yaml.v3"
)

type Task struct {
	IP       string
	Port     int
	Executor func(ip string, port int)
}

type WorkerPool struct {
	workerCount int
	taskQueue   chan Task
	wg          sync.WaitGroup
}

func NewWorkerPool(workerCount, bufferSize int) *WorkerPool {
	return &WorkerPool{
		workerCount: workerCount,
		taskQueue:   make(chan Task, bufferSize),
	}
}

func (wp *WorkerPool) Start() {
	for i := 0; i < wp.workerCount; i++ {
		wp.wg.Add(1)
		go func() {
			defer wp.wg.Done()
			for task := range wp.taskQueue {
				task.Executor(task.IP, task.Port)
			}
		}()
	}
}

func (wp *WorkerPool) AddTask(task Task) {
	wp.taskQueue <- task
}

func (wp *WorkerPool) Close() {
    close(wp.taskQueue)
}

func (wp *WorkerPool) Wait() {
	wp.wg.Wait()
}