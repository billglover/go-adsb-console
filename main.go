package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/streadway/amqp"
)

// Aircraft holds all known details about a flight.
// Property definitions: https://github.com/SDRplay/dump1090/blob/master/README-json.md
type Aircraft struct {
	Flight    string  `json:"flight"`    // the flight name / callsign
	Lon       float64 `json:"lon"`       // the aircraft longitude in decimal degrees
	Lat       float64 `json:"lat"`       // // the aircraft latitude in decimal degrees
	Track     int     `json:"track"`     // true track over ground in degrees (0-359)
	Speed     int     `json:"speed"`     // reported speed in kt. This is usually speed over ground, but might be IAS - you can't tell the difference here, sorry!
	Hex       string  `json:"hex"`       // the 24-bit ICAO identifier of the aircraft, as 6 hex digits. The identifier may start with '~', this means that the address is a non-ICAO address (e.g. from TIS-B).
	Squawk    string  `json:"squawk"`    // the 4-digit squawk (octal representation)
	Seen      float64 `json:"seen"`      // how long ago (in seconds before "now") a message was last received from this aircraft
	SeenPos   float64 `json:"seen_pos"`  // how long ago (in seconds before "now") the position was last updated
	Messages  int     `json:"messages"`  // total number of Mode S messages received from this aircraft
	Category  string  `json:"category"`  // the NUCp (navigational uncertainty category) reported for the position TODO: Check this value!
	Timestamp int64   `json:"timestamp"` // the timestamp ("now") when this record was created
	Altitude  int     `json:"altitude"`  // the aircraft altitude in feet, or "ground" if it is reporting it is on the ground
	VertRate  int     `json:"vert_rate"` // vertical rate in feet/minute
	Rssi      float64 `json:"rssi"`      // recent average RSSI (signal power), in dbFS; this will always be negative
	Type      string  `json:"type"`      // the type of vessel being tracked
}

// Scan holds flight details for all currently visible aircraft.
type Scan struct {
	Now      float64    `json:"now"`      // the time this file was generated, in seconds since Jan 1 1970 00:00:00 GMT (the Unix epoch)
	Messages int        `json:"messages"` // the total number of Mode S messages processed since scanning started
	Aircraft []Aircraft `json:"aircraft"` // a slice of Aircraft, one entry for each known aircraft
}

// Store is an in memory map of aircraft
type Store struct {
	stale    bool
	lock     *sync.Mutex
	aircraft map[string]Aircraft
}

func main() {
	fName := flag.String("aircraft", "aircraft.json", "path to the aircraft.json file to monitor")
	mDur := flag.Duration("monitorFreq", time.Second*1, "duration between polling for updates")
	uDur := flag.Duration("updateFreq", time.Second*5, "maximum duration between updates to RabbitMQ")

	flag.Parse()

	info, err := os.Stat(*fName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to open file: %v\n", err)
		os.Exit(1)
	}

	conn, err := amqp.Dial("amqp://guest:guest@localhost:5672/")
	failOnError(err, "Failed to connect to RabbitMQ")
	defer conn.Close()

	rmqCh, err := conn.Channel()
	failOnError(err, "Failed to open a channel")
	defer rmqCh.Close()

	rmqCh.ExchangeDeclare(
		"adsb-fan-exchange", // name
		"fanout",            // kind
		false,               // durable
		false,               // delete when unused
		false,               // exclusive
		false,               // no-wait
		nil,                 // arguments
	)

	fmt.Printf("Reading file: %s\n", info.Name())

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	defer func() {
		signal.Stop(c)
		cancel()
	}()

	go func() {
		select {
		case <-c:
			cancel()
		case <-ctx.Done():
		}
	}()

	var store = Store{
		stale:    false,
		aircraft: make(map[string]Aircraft),
		lock:     new(sync.Mutex),
	}

	go updateFlights(ctx, rmqCh, "adsb-fan-exchange", *uDur, &store)
	monitorFlights(ctx, *fName, *mDur, &store)
}

func updateFlights(ctx context.Context, rmqCh *amqp.Channel, ex string, dur time.Duration, store *Store) {

	ticker := time.NewTicker(dur)
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
			failOnError(err, "Failed to marshal scan")

			msg := amqp.Publishing{
				DeliveryMode: amqp.Transient,
				Timestamp:    time.Now(),
				ContentType:  "application/json",
				Body:         body,
			}

			store.lock.Lock()
			if store.stale {
				store.stale = false
				err = rmqCh.Publish(ex, "", false, false, msg)
				if err != nil {
					fmt.Fprintf(os.Stderr, "unable to publish to exchange: %v\n", err)
					store.stale = true
				}
			}
			store.lock.Unlock()
		}
	}

}

func monitorFlights(ctx context.Context, fName string, d time.Duration, store *Store) {
	ticker := time.NewTicker(d).C

	var lastModified time.Time

	for {
		select {
		case <-ticker:
			info, err := os.Stat(fName)
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to stat file: %v\n", err)
			}

			if info.ModTime().After(lastModified) {
				lastModified = info.ModTime()

				f, err := os.Open(fName)
				if err != nil {
					fmt.Fprintf(os.Stderr, "failed to open file: %v\n", err)
				}

				err = updateAircraft(f, store)
				if err != nil {
					fmt.Fprintf(os.Stderr, "failed to update aircraft position: %v\n", err)
				}
			}
		case <-ctx.Done():
			fmt.Println("terminating file watcher")
			return
		}
	}
}

func updateAircraft(r io.Reader, store *Store) error {
	dec := json.NewDecoder(r)
	scan := new(Scan)
	err := dec.Decode(scan)
	if err != nil {
		return err
	}

	for _, a1 := range scan.Aircraft {
		a2, ok := store.aircraft[a1.Flight]
		if ok && !hasMoved(a1, a2) {
			continue
		}

		store.aircraft[a1.Flight] = a1

		store.lock.Lock()
		store.stale = true
		store.lock.Unlock()

		fmt.Println(store.aircraft[a1.Flight])
	}

	return nil
}

func hasMoved(a1, a2 Aircraft) bool {
	return !(a1.Lon == a2.Lon &&
		a1.Lat == a2.Lat &&
		a1.Altitude == a2.Altitude &&
		a1.Track == a2.Track)
}

func failOnError(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %s", msg, err)
	}
}
