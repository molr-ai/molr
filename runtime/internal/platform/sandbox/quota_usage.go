package sandbox

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"molr.ai/runtime/internal/core"
	"molr.ai/runtime/internal/policy"
	"molr.ai/runtime/internal/quota"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type ComputeUsage struct {
	MemoryBytes          int64
	Processes            int64
	CPUUsageMicroseconds int64
}

func (r *Runner) NetworkUsage(ctx context.Context, owner, assistant core.ID, source string, v policy.NetworkConstraints) (quota.NetworkUsage, error) {
	return r.quotas.Usage(ctx, string(owner)+"/"+string(assistant)+"/"+source, v)
}
func (r *Runner) ComputeUsage(ctx context.Context, owner, assistant core.ID) ComputeUsage {
	sum := sha256.Sum256([]byte(string(owner) + "/" + string(assistant)))
	root := filepath.Join(r.options.CgroupRoot, "quota-"+hex.EncodeToString(sum[:16]))
	account := sha256.Sum256([]byte(string(owner)))
	groupRoot := filepath.Join(r.options.CgroupRoot, "account-"+hex.EncodeToString(account[:16]), "quota-"+hex.EncodeToString(sum[:16]))
	if _, err := os.Stat(groupRoot); err == nil {
		root = groupRoot
	}
	number := func(name string) int64 {
		b, _ := os.ReadFile(filepath.Join(root, name))
		v, _ := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
		return v
	}
	out := ComputeUsage{MemoryBytes: number("memory.current"), Processes: number("pids.current")}
	b, _ := os.ReadFile(filepath.Join(root, "cpu.stat"))
	fields := strings.Fields(string(b))
	for i := 0; i+1 < len(fields); i += 2 {
		if fields[i] == "usage_usec" {
			out.CPUUsageMicroseconds, _ = strconv.ParseInt(fields[i+1], 10, 64)
		}
	}
	return out
}
