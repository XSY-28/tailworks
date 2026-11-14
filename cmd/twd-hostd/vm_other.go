//go:build !linux

package main

import (
	"log/slog"
	"time"

	"github.com/rosskukulinski/twd/hostd/sandbox"
)

type vmOptions struct {
	kernel, rootfs, firecracker, jailer, helper, hugePages string
	memMiB, vcpus, pool                                    int
	snapshot, snapRefill                                   bool
	rotate                                                 time.Duration
}

// vmBackend: the microVM tier needs Linux + KVM; on this OS every workload
// runs in the process tier.
func vmBackend(vmOptions, *slog.Logger) sandbox.Backend { return nil }
