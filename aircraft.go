package main

import (
	"errors"
	"strings"
	"sync"
	"time"
)

// Aircraft holds all known details about a flight.
// Property definitions: https://github.com/SDRplay/dump1090/blob/master/README-json.md
type Aircraft struct {
	Flight      string  `json:"flight"`              // the flight name / callsign
	Lon         float64 `json:"lon"`                 // the aircraft longitude in decimal degrees
	Lat         float64 `json:"lat"`                 // // the aircraft latitude in decimal degrees
	Track       int     `json:"track"`               // true track over ground in degrees (0-359)
	Speed       int     `json:"speed,omitempty"`     // reported speed in kt. This is usually speed over ground, but might be IAS - you can't tell the difference here, sorry!
	Hex         string  `json:"hex"`                 // the 24-bit ICAO identifier of the aircraft, as 6 hex digits. The identifier may start with '~', this means that the address is a non-ICAO address (e.g. from TIS-B).
	Squawk      string  `json:"squawk,omitempty"`    // the 4-digit squawk (octal representation)
	Seen        float64 `json:"seen,omitempty"`      // how long ago (in seconds before "now") a message was last received from this aircraft
	SeenPos     float64 `json:"seen_pos,omitempty"`  // how long ago (in seconds before "now") the position was last updated
	Messages    int     `json:"messages,omitempty"`  // total number of Mode S messages received from this aircraft
	Category    string  `json:"category,omitempty"`  // indicates what type of transmission equipment is on board: Class A1, A1S, A2, A3, B1S, or B1 equipment
	NUCP        int     `json:"nucp,omitempty"`      // the NUCp (navigational uncertainty category) reported for the position
	Timestamp   int64   `json:"timestamp,omitempty"` // the timestamp ("now") when this record was created
	Altitude    int     `json:"altitude"`            // the aircraft altitude in feet, or "ground" if it is reporting it is on the ground
	VertRate    int     `json:"vert_rate,omitempty"` // vertical rate in feet/minute
	Rssi        float64 `json:"rssi,omitempty"`      // recent average RSSI (signal power), in dbFS; this will always be negative
	Type        string  `json:"type"`                // the type of vessel being tracked
	StationName string  `json:"groundStationName"`   // ground station name used to identify the receiver
}

// Scan holds flight details for all currently visible aircraft.
type Scan struct {
	Now      float64    `json:"now"`      // the time this file was generated, in seconds since Jan 1 1970 00:00:00 GMT (the Unix epoch)
	Messages int        `json:"messages"` // the total number of Mode S messages processed since scanning started
	Aircraft []Aircraft `json:"aircraft"` // a slice of Aircraft, one entry for each known aircraft
}

// AircraftPos is a record that maintains the last known position of an aircraft
type AircraftPos struct {
	modified bool
	aircraft Aircraft
}

// Store is an in memory map of aircraft
type Store struct {
	lock     *sync.Mutex
	aircraft map[string]AircraftPos
}

// HasMoved takes two Aircraft positions and returns a boolean to indicate
// whether the aircraft has moved. An error is returned if the positions
// provided relate to different aircraft.
func HasMoved(a1, a2 Aircraft) (bool, error) {
	if a1.Flight == "" || a2.Flight == "" {
		return false, errors.New("a1 and/or a2 represents unknown aircraft")
	}

	if a1.Flight != a2.Flight {
		return false, errors.New("a1 and a2 represent different aircraft")
	}

	return !(a1.Lon == a2.Lon &&
		a1.Lat == a2.Lat &&
		a1.Altitude == a2.Altitude &&
		a1.Track == a2.Track), nil
}

// UpdateAircraft takes a Scan and updates the data Store with the latest
// aircraft positions. Aircraft positions older than maxAge are removed
// from the data Store. The data Store is marked as modified if changes
// are made.
func updateAircraft(s Scan, store *Store, station string) {

	// update aircraft positions in the data Store
	for i := range s.Aircraft {

		if s.Aircraft[i].Flight == "" || s.Aircraft[i].Lon == 0 || s.Aircraft[i].Lat == 0 {
			continue
		}

		// Update and clean the aircraft data
		s.Aircraft[i].Flight = strings.TrimSpace(s.Aircraft[i].Flight)
		s.Aircraft[i].Type = "AIRCRAFT"
		s.Aircraft[i].StationName = station
		if s.Aircraft[i].Timestamp == 0 {
			s.Aircraft[i].Timestamp = time.Now().Unix()
		}

		a2, ok := store.aircraft[s.Aircraft[i].Flight]
		moved, _ := HasMoved(s.Aircraft[i], a2.aircraft)
		if ok && !moved {
			continue
		}

		store.lock.Lock()
		store.aircraft[s.Aircraft[i].Flight] = AircraftPos{aircraft: s.Aircraft[i], modified: true}
		store.lock.Unlock()
	}
}

// PurgeAircraft removes any aircraft not present in the scan from the
// data Store. Any aircraft that are included in the scan but are older
// than maxAge are also removed.
func purgeAircraft(s Scan, store *Store, maxAge time.Duration) {
	seen := map[string]bool{}
	for _, a := range s.Aircraft {
		seen[a.Flight] = true
	}

	for k, v := range store.aircraft {

		if _, ok := seen[k]; ok != true {
			delete(store.aircraft, k)
			continue
		}

		lastSeen := time.Second * time.Duration(v.aircraft.Seen)
		if lastSeen > maxAge {
			delete(store.aircraft, k)
		}
	}
}
