//go:build linux

package vm

import (
	"fmt"
	"os"
	"strings"
)

// procRSSKB is pid's resident memory in kB: VmRSS plus hugetlb pages
// (guest memory backed by huge pages is not counted in VmRSS).
func procRSSKB(pid int) int64 {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	var total int64
	for _, ln := range strings.Split(string(b), "\n") {
		for _, k := range []string{"VmRSS:", "HugetlbPages:"} {
			if strings.HasPrefix(ln, k) {
				var n int64
				fmt.Sscan(strings.TrimSpace(strings.TrimPrefix(ln, k)), &n)
				total += n
			}
		}
	}
	return total
}

// FreeHugePages2M returns the host's available 2 MiB hugetlbfs pages:
// free minus reserved. A VM's guest RAM is reserved when Firecracker maps it
// and only counts as used once the guest touches it, so free_hugepages
// alone overstates what the next VM can get.
func FreeHugePages2M() int {
	read := func(name string) int {
		b, err := os.ReadFile("/sys/kernel/mm/hugepages/hugepages-2048kB/" + name)
		if err != nil {
			return 0
		}
		var n int
		fmt.Sscan(strings.TrimSpace(string(b)), &n)
		return n
	}
	return max(0, read("free_hugepages")-read("resv_hugepages"))
}
