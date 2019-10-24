package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"sync"
	"time"
)

var (
	// GitRevision is set by the build process
	GitRevision string
)

func main() {
	var monitorDuration time.Duration = time.Second * 1
	var updateDuration time.Duration = time.Second * 5
	var aircraftJSON string = "/run/dump1090-mutability/aircraft.json"
	var maxAircraftAge time.Duration = time.Second * 60
	var amqpURL string
	var amqpExchange string = "adsb-fan-exchange"
	var stationName string

	flag.StringVar(&aircraftJSON, "aircraft", LookupEnvOrString("ADSB_AIRCRAFT_JSON", aircraftJSON), "location of the aircraft.json file to monitor")
	flag.DurationVar(&maxAircraftAge, "max-aircraft-age", LookupEnvOrDur("ADSB_MAX_AIRCRAFT_AGE", maxAircraftAge), "maximum age for an aircraft before it is removed from memory")
	flag.DurationVar(&monitorDuration, "monitor-every", LookupEnvOrDur("ADSB_MONITOR_EVERY", monitorDuration), "duration between polling for aircraft movement")
	flag.DurationVar(&updateDuration, "update-every", LookupEnvOrDur("ADSB_UPDATE_EVERY", updateDuration), "duration between sending an updated aircraft scan")
	flag.StringVar(&amqpURL, "amqp-url", LookupEnvOrString("ADSB_RABBITMQ_URL", amqpURL), "connection string for RabbitMQ")
	flag.StringVar(&amqpExchange, "amqp-exchange", LookupEnvOrString("ADSB_RABBITMQ_EXCHANGE", amqpExchange), "exchange name for RabbitMQ")
	flag.StringVar(&stationName, "station-name", LookupEnvOrString("ADSB_BASE_STATION_NAME", stationName), "friendly name for the base station (note: publicly visible)")
	flag.Parse()

	// Handle OS signals gracefully
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

	// Create an in-memory store to hold the latest aircraft positions
	var store = Store{
		aircraft: make(map[string]AircraftPos),
		lock:     new(sync.Mutex),
	}

	// Start monitoring for aircraft positions
	err := startMonitor(ctx, aircraftJSON, monitorDuration, maxAircraftAge, &store, stationName)
	if err != nil {
		log.Fatalln("failed to start monitor:", err)
	}

	// Start sending updates to RabbitMQ
	err = startUpdater(ctx, amqpURL, amqpExchange, updateDuration, stationName, &store)
	if err != nil {
		log.Fatalln("failed to start updater:", err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		}
	}
}

// LookupEnvOrString returns the value of the provided environment variable if
// set. If the environment variable is not set, then the initial string value
// is returned instead.
func LookupEnvOrString(key string, initial string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return initial
}

// LookupEnvOrDur returns the value of the provided environment variable if
// set. If the environment variable is not set or results in an error during
// parsing, then the initial duration is returned instead.
func LookupEnvOrDur(key string, initial time.Duration) time.Duration {
	if val, ok := os.LookupEnv(key); ok {
		dur, err := time.ParseDuration(val)
		if err != nil {
			return initial
		}

		return dur
	}
	return initial
}
