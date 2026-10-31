
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"time"

	"latihan-fiber/app/model"
	"latihan-fiber/config"
	"latihan-fiber/database"

	"github.com/hibiken/asynq"
)

func main() {
	outboxID := flag.Int64("outbox", 0, "ID outbox")
	copies := flag.Int("copies", 5, "Jumlah salinan task")
	flag.Parse()

	if *outboxID <= 0 || *copies <= 0 {
		panic("gunakan -outbox dan -copies dengan nilai positif")
	}

	config.LoadEnv()

	ctx := context.Background()
	pool, err := database.NewPool(ctx)
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	var eventType string
	var eventPayload []byte

	err = pool.QueryRow(ctx,
		`SELECT event_type, payload FROM outbox WHERE id = $1`,
		*outboxID,
	).Scan(&eventType, &eventPayload)
	if err != nil {
		panic(err)
	}

	payload, err := json.Marshal(model.NotificationJob{
		OutboxID:  *outboxID,
		EventType: eventType,
		Payload:   json.RawMessage(eventPayload),
	})
	if err != nil {
		panic(err)
	}

	client := asynq.NewClient(config.RedisOpt())
	defer client.Close()

	for i := 1; i <= *copies; i++ {
		taskID := fmt.Sprintf("outbox-%d-copy-%d", *outboxID, i)

		task := asynq.NewTask(
			model.TaskSendNotification,
			payload,
			asynq.TaskID(taskID),
			asynq.Queue(model.QueueNotifications),
			asynq.MaxRetry(5),
			asynq.Timeout(30*time.Second),
			asynq.Retention(24*time.Hour),
		)

		info, err := client.EnqueueContext(ctx, task)
		if err != nil {
			panic(err)
		}

		fmt.Printf("Task berhasil dibuat: %s\n", info.ID)
	}

	fmt.Printf("Selesai: %d salinan untuk outbox %d\n", *copies, *outboxID)
}
