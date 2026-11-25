package wallclock

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
)

// The test hook (only a `-tags twtestclock` build turns it on: testhook_on.go).
//
//	TW_TEST_WALL_CLOCK_FILE=<path>   the offset, as a Go duration ("5m",
//	                                 "-3m30s", "0s"). Re-read every 100 ms,
//	                                 so a test steps the clock by rewriting
//	                                 it. A missing or empty file is 0.
//	TW_TEST_FAKETIME_LIB=<path>      libfaketime (libfaketime.so.1). When
//	                                 set, every workerd hostd starts runs
//	                                 with it preloaded and follows the same
//	                                 offset, through <path>.faketime.
//
// The offset is rounded to whole seconds (libfaketime's file format), the
// same for hostd and workerd. On a step, workerd's view changes within
// about a second (libfaketime re-reads its file once a second) and hostd's
// right after the file is written. Monotonic clocks are never shifted:
// a wrong wall clock doesn't make a machine's timers run fast or slow.

// pollEvery is how often follow re-reads the offset file.
const pollEvery = 100 * time.Millisecond

// follow sets the offset from path now and keeps it in step with the file
// until stop is closed. lib, if set, is libfaketime: workerds get it
// preloaded, and the offset is mirrored to path+".faketime" (written
// before hostd's own offset changes) for it to read.
func follow(path, lib string, stop <-chan struct{}) error {
	ft := path + ".faketime"
	last := time.Duration(-1 << 63)
	apply := func() error {
		d, err := readOffset(path)
		if err != nil {
			return err
		}
		if d == last {
			return nil
		}
		if lib != "" {
			if err := writeAtomic(ft, faketimeSpec(d)); err != nil {
				return err
			}
		}
		offset.Store(int64(d))
		last = d
		return nil
	}
	if err := apply(); err != nil {
		return err
	}
	if lib != "" {
		env := []string{"FAKETIME_TIMESTAMP_FILE=" + ft, "FAKETIME_DONT_FAKE_MONOTONIC=1", "FAKETIME_CACHE_DURATION=1"}
		if runtime.GOOS == "darwin" {
			env = append(env, "DYLD_INSERT_LIBRARIES="+lib, "DYLD_FORCE_FLAT_NAMESPACE=1")
		} else {
			env = append(env, "LD_PRELOAD="+lib)
		}
		workerd = func() ([]string, []string) { return env, []string{lib, ft} }
	}
	go func() {
		t := time.NewTicker(pollEvery)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				apply() // a half-written or bad file keeps the last offset
			}
		}
	}()
	return nil
}

// readOffset parses the offset file: a Go duration, rounded to seconds.
func readOffset(path string) (time.Duration, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("wall clock offset %q: %w", s, err)
	}
	return d.Round(time.Second), nil
}

// faketimeSpec is d in libfaketime's relative format: "+300", "-90".
func faketimeSpec(d time.Duration) string {
	return fmt.Sprintf("%+d\n", int64(d/time.Second))
}

func writeAtomic(path, s string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(s), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
