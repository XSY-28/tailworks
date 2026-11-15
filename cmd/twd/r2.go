package main

// R2 buckets (ADR-r2-buckets), CLI side: `twd r2 bucket ls|create|rm` and
// `twd r2 object ls`. A bucket is a platform store of kind "r2", so these
// use mgmt's stores API (ADR-platform-stores); object ls reads the
// bucket's journal (its index) and folds it into the live keys. Object
// bytes live in the hosts' R2 backend, which mgmt can't reach: object
// get/put from the CLI is not built yet.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/rosskukulinski/twd/hostd/bundle"
	"github.com/rosskukulinski/twd/hostd/mgmtapi"
)

// r2Bucket is a bucket as the stores API describes it.
type r2Bucket struct {
	Name      string `json:"name"`
	Head      int64  `json:"head"`
	Entries   int64  `json:"entries"`
	Bytes     int64  `json:"bytes"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

// r2Object is one live key of a bucket's index.
type r2Object struct {
	Key         string `json:"key"`
	Size        int64  `json:"size"`
	ETag        string `json:"etag"`
	Uploaded    int64  `json:"uploaded"`
	ContentType string `json:"contentType,omitempty"`
}

type r2Result struct {
	Workload string     `json:"workload"`
	TenantID string     `json:"tenantID"`
	Bucket   string     `json:"bucket,omitempty"`
	Buckets  []r2Bucket `json:"buckets,omitempty"`
	Objects  []r2Object `json:"objects,omitempty"`
	Created  bool       `json:"created,omitempty"`
	Deleted  bool       `json:"deleted,omitempty"`
}

const r2Usage = "r2 bucket ls | r2 bucket create <bucket> | r2 bucket rm <bucket> | r2 object ls <bucket> [--prefix P]   [--workload <workload>] [--tenant <tailnet>]"

func cmdR2(args []string) error {
	// Global flags (--state-dir X, --json) may precede the subcommands.
	var pos []int
	for i := 0; i < len(args) && len(pos) < 2; i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			pos = append(pos, i)
			continue
		}
		if (a == "--state-dir" || a == "-state-dir") && i+1 < len(args) {
			i++
		}
	}
	if len(pos) < 2 {
		return fail("bad_request", "twd r2 needs a subcommand: bucket ls|create|rm, object ls", "usage: twd "+r2Usage)
	}
	what, sub := args[pos[0]], args[pos[1]]
	rest := append(append(append([]string{}, args[:pos[0]]...), args[pos[0]+1:pos[1]]...), args[pos[1]+1:]...)
	switch sub {
	case "list":
		sub = "ls"
	case "delete":
		sub = "rm"
	}
	f := newFlags("r2")
	tenantF := tenantFlag(f)
	workloadF := f.String("workload", "", "workload (default: the app in the current directory, as twd deploy names it)")
	prefixF := f.String("prefix", "", "object ls: only keys starting with this")
	p, err := f.parse(rest)
	if err != nil {
		return err
	}
	switch {
	case what == "bucket" && sub == "ls" && len(p) == 0:
	case what == "bucket" && (sub == "create" || sub == "rm") && len(p) == 1:
	case what == "object" && sub == "ls" && len(p) == 1:
	case what == "object" && (sub == "get" || sub == "put"):
		return fail("not_supported", "twd r2 object "+sub+" is not built yet: object bytes live in the hosts' R2 backend, which mgmt can't reach",
			"read or write objects through the workload's binding (env.<BUCKET>) for now")
	default:
		return fail("bad_request", fmt.Sprintf("unknown or incomplete command: twd r2 %s %s", what, sub), "usage: twd "+r2Usage)
	}
	bucket := ""
	if len(p) == 1 {
		bucket = p[0]
		if !bundle.StoreNameRE.MatchString(bucket) {
			return fail("bad_request", fmt.Sprintf("bucket name %q must match %s", bucket, bundle.StoreNameRE))
		}
	}
	workload := *workloadF
	if workload == "" {
		a, err := loadApp(".", "")
		if err != nil {
			return fail("bad_request", "no app in the current directory to take the workload from").
				withFix("cd <app dir> && twd r2 …", "twd r2 … --workload <workload>")
		}
		workload = a.Name
	}
	if workloadLabel(workload) != workload {
		return fail("bad_request", fmt.Sprintf("workload %q is not a DNS label", workload))
	}
	s, err := openState(*f.stateDir)
	if err != nil {
		return err
	}
	if err := s.requireUp(); err != nil {
		return err
	}
	tn, err := s.tenant(*tenantF)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	base := "/v1/tenants/" + url.PathEscape(tn.TenantID) + "/workloads/" + url.PathEscape(workload) + "/stores"
	res := r2Result{Workload: workload, TenantID: tn.TenantID, Bucket: bucket}
	hdr := map[string]string{"Twd-Actor": actor()}
	switch what + " " + sub {
	case "bucket ls":
		items, err := listAll[r2Bucket](ctx, s.mgmt(), base+"?kind=r2", 0)
		if err != nil {
			return err
		}
		res.Buckets = items
		if res.Buckets == nil {
			res.Buckets = []r2Bucket{}
		}
		emit(f, res, func() {
			if len(items) == 0 {
				fmt.Printf("no R2 buckets for workload %s (tenant %s) · a bucket appears on its first write, or: twd r2 bucket create <bucket>\n", workload, tn.Tailnet)
				return
			}
			fmt.Printf("%-32s %10s %-20s\n", "BUCKET", "INDEX", "UPDATED")
			for _, b := range items {
				fmt.Printf("%-32s %10s %-20s\n", b.Name, humanBytes(b.Bytes), time.UnixMilli(b.UpdatedAt).Local().Format("2006-01-02 15:04:05"))
			}
		})
	case "bucket create":
		var st struct {
			CreatedAt int64 `json:"createdAt"`
			UpdatedAt int64 `json:"updatedAt"`
			Head      int64 `json:"head"`
		}
		if err := s.mgmt().DoHeaders(ctx, "PUT", base+"/r2/"+url.PathEscape(bucket), map[string]any{}, &st, hdr); err != nil {
			return err
		}
		res.Created = st.Head == 0 && st.CreatedAt == st.UpdatedAt
		emit(f, res, func() {
			fmt.Printf("✓ R2 bucket %s ready for workload %s (tenant %s)\n", bucket, workload, tn.Tailnet)
			fmt.Printf("  bind it in wrangler.jsonc: \"r2_buckets\": [{ \"binding\": \"BUCKET\", \"bucket_name\": %q }]\n", bucket)
		})
	case "bucket rm":
		if err := s.mgmt().DoHeaders(ctx, "DELETE", base+"/r2/"+url.PathEscape(bucket), nil, nil, hdr); err != nil {
			return err
		}
		res.Deleted = true
		emit(f, res, func() {
			fmt.Printf("✓ R2 bucket %s of workload %s deleted (tenant %s)\n", bucket, workload, tn.Tailnet)
			fmt.Printf("  its index is gone now; the workload drops its blobs the next time it uses the bucket\n")
		})
	case "object ls":
		objs, err := r2ListObjects(ctx, s.mgmt(), base+"/r2/"+url.PathEscape(bucket)+"/journal", *prefixF)
		if err != nil {
			return err
		}
		res.Objects = objs
		emit(f, res, func() {
			if len(objs) == 0 {
				fmt.Printf("no objects in R2 bucket %s of workload %s\n", bucket, workload)
				return
			}
			fmt.Printf("%-48s %10s %-20s %s\n", "KEY", "SIZE", "UPLOADED", "ETAG")
			for _, o := range objs {
				fmt.Printf("%-48s %10s %-20s %s\n", o.Key, humanBytes(o.Size), time.UnixMilli(o.Uploaded).Local().Format("2006-01-02 15:04:05"), o.ETag)
			}
		})
	}
	return nil
}

// r2ListObjects reads a bucket's journal (admin) and folds its entries
// (hostd/gen/js/store-r2.js: put and del by key) into the live keys.
func r2ListObjects(ctx context.Context, c *mgmtapi.Client, journal, prefix string) ([]r2Object, error) {
	live := map[string]r2Object{}
	after := int64(0)
	for {
		var p mgmtapi.StoreJournalPage
		if err := c.Do(ctx, "GET", fmt.Sprintf("%s?after=%d", journal, after), nil, &p); err != nil {
			return nil, err
		}
		for _, e := range p.Entries {
			var en struct {
				O   string `json:"o"`
				K   string `json:"k"`
				Row struct {
					Size     int64             `json:"size"`
					ETag     string            `json:"etag"`
					Uploaded int64             `json:"uploaded"`
					HTTP     map[string]string `json:"http"`
				} `json:"row"`
			}
			if json.Unmarshal(e.Data, &en) != nil {
				continue
			}
			switch en.O {
			case "put":
				live[en.K] = r2Object{Key: en.K, Size: en.Row.Size, ETag: en.Row.ETag, Uploaded: en.Row.Uploaded, ContentType: en.Row.HTTP["contentType"]}
			case "del":
				delete(live, en.K)
			}
			after = e.Seq
		}
		if !p.More || len(p.Entries) == 0 {
			break
		}
	}
	out := make([]r2Object, 0, len(live))
	for k, o := range live {
		if strings.HasPrefix(k, prefix) {
			out = append(out, o)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func humanBytes(n int64) string {
	const u = 1024
	if n < u {
		return fmt.Sprintf("%d B", n)
	}
	d, e := int64(u), 0
	for m := n / u; m >= u; m /= u {
		d *= u
		e++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(d), "KMGTPE"[e])
}
