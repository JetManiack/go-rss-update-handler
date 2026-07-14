package bus

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisBus struct {
	client *redis.Client
}

func NewRedisBus(client *redis.Client) *RedisBus {
	return &RedisBus{client: client}
}

func (b *RedisBus) Publish(ctx context.Context, topic string, msg Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	return b.client.XAdd(ctx, &redis.XAddArgs{
		Stream: topic,
		Values: map[string]interface{}{
			"data": data,
		},
	}).Err()
}

func (b *RedisBus) Subscribe(ctx context.Context, topic, group string, handler Handler) error {
	// Create the group, ignore the error if it already exists
	_ = b.client.XGroupCreateMkStream(ctx, topic, group, "0").Err()

	for {
		streams, err := b.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    group,
			Consumer: "worker-1", // Should be made dynamic in the future
			Streams:  []string{topic, ">"},
			Count:    1,
			Block:    0,
		}).Result()

		if err != nil {
			if err == context.Canceled {
				return nil
			}
			time.Sleep(time.Second)
			continue
		}

		for _, stream := range streams {
			for _, message := range stream.Messages {
				var msg Message
				data := message.Values["data"].(string)
				if err := json.Unmarshal([]byte(data), &msg); err != nil {
					continue
				}

				if err := handler(ctx, msg); err == nil {
					b.client.XAck(ctx, topic, group, message.ID)
				}
			}
		}
	}
}
