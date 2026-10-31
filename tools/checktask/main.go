package main

import (
	"fmt"
	"latihan-fiber/config"

	"github.com/hibiken/asynq"
)

func main() {
	inspector := asynq.NewInspector(config.RedisOpt())
	defer inspector.Close()

	// 1. Periksa statistik queue
	infoQueue, err := inspector.GetQueueInfo("notifications")
	if err != nil {
		panic(err)
	}

	fmt.Printf("Queue: %s\n", infoQueue.Queue)
	fmt.Printf("Active: %d\n", infoQueue.Active)
	fmt.Printf("Pending: %d\n", infoQueue.Pending)
	fmt.Printf("Retry: %d\n", infoQueue.Retry)
	fmt.Printf("Archived: %d\n", infoQueue.Archived)

	// 2. Periksa daftar task Archived
	archived, err := inspector.ListArchivedTasks("notifications")
	if err != nil {
		panic(err)
	}

	fmt.Println("\nDaftar Archived:")
	for _, task := range archived {
		fmt.Printf(
			"ID: %s | State: %s | Retried: %d/%d | LastErr: %s\n",
			task.ID,
			task.State,
			task.Retried,
			task.MaxRetry,
			task.LastErr,
		)
	}

	// Periksa daftar task Active
	active, err := inspector.ListActiveTasks("notifications")
	if err != nil {
		panic(err)
	}

	fmt.Println("\nDaftar Active:")
	for _, task := range active {
		fmt.Printf(
			"ID: %s | State: %s | Retried: %d/%d | LastErr: %s\n",
			task.ID,
			task.State,
			task.Retried,
			task.MaxRetry,
			task.LastErr,
		)
	}

	// 4. Periksa detail task setelah dijadwalkan ulang
	info, err := inspector.GetTaskInfo("notifications", "outbox-7")
	if err != nil {
		panic(err)
	}

	fmt.Printf("\nID: %s\n", info.ID)
	fmt.Printf("State: %s\n", info.State)
	fmt.Printf("IsOrphaned: %v\n", info.IsOrphaned)
	fmt.Printf("Retried: %d/%d\n", info.Retried, info.MaxRetry)
	fmt.Printf("LastErr: %s\n", info.LastErr)
}
