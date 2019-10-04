package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/streadway/amqp"
)

func startUpdater(ctx context.Context, conStr, exchange string, dur time.Duration, station string, store *Store) error {

	conn, err := amqp.Dial(conStr)
	if err != nil {
		return fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	rmqCh, err := conn.Channel()
	if err != nil {
		return fmt.Errorf("failed to open a channel: %w", err)
	}

	rmqCh.ExchangeDeclare(
		exchange, // name
		"fanout", // kind
		false,    // durable
		false,    // delete when unused
		false,    // exclusive
		false,    // no-wait
		nil,      // arguments
	)

	ticker := time.NewTicker(dur)

	go func() {
		defer conn.Close()
		defer rmqCh.Close()
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return

			case <-ticker.C:
				a := make([]Aircraft, len(store.aircraft))
				i := 0
				for _, v := range store.aircraft {
					a[i] = v
					i++
				}

				s := Scan{
					Now:      float64(time.Now().UnixNano()) / 1000000000,
					Aircraft: a,
				}

				body, err := json.Marshal(s)
				if err != nil {
					fmt.Fprintf(os.Stderr, "failed to marshal Scan: %v\n", err)
				}
				msg := amqp.Publishing{
					DeliveryMode: amqp.Transient,
					Timestamp:    time.Now(),
					ContentType:  "application/json",
					Body:         body,
				}

				store.lock.Lock()
				if store.modified {
					store.modified = false
					err = rmqCh.Publish(exchange, "", false, false, msg)
					if err != nil {
						fmt.Fprintf(os.Stderr, "failed to publish to exchange: %v\n", err)
						store.modified = true
					}
				}
				store.lock.Unlock()
			}
		}
	}()

	return nil
}
