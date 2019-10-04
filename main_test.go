package main

import (
	"os"
	"testing"
	"time"
)

func TestLookupEnvOrString(t *testing.T) {

	t.Run("default", func(t *testing.T) {
		want := "dummy_default"
		got := LookupEnvOrString("TEST_STRING_VAR", want)
		if got != want {
			t.Errorf("%s != %s", got, want)
		}
	})

	t.Run("envvar", func(t *testing.T) {
		want := "dummy_envar"
		os.Setenv("TEST_STRING_VAR", want)
		got := LookupEnvOrString("TEST_STRING_VAR", "invalid")
		if got != want {
			t.Errorf("%s != %s", got, want)
		}
	})
}

func TestLookupEnvOrDur(t *testing.T) {

	t.Run("default", func(t *testing.T) {
		want := time.Minute * 10
		got := LookupEnvOrDur("TEST_DURATION_VAR", want)
		if got != want {
			t.Errorf("%s != %s", got, want)
		}
	})

	t.Run("envvar", func(t *testing.T) {
		want := time.Minute * 10
		os.Setenv("TEST_DURATION_VAR", want.String())
		got := LookupEnvOrDur("TEST_DURATION_VAR", time.Hour*5)
		if got != want {
			t.Errorf("%s != %s", got, want)
		}
	})

	t.Run("error", func(t *testing.T) {
		want := time.Minute * 10
		os.Setenv("TEST_DURATION_VAR", "invalid")
		got := LookupEnvOrDur("TEST_DURATION_VAR", want)
		if got != want {
			t.Errorf("%s != %s", got, want)
		}
	})
}
