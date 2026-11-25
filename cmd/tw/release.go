package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rosskukulinski/tailworks/internal/catalog"
	"github.com/rosskukulinski/tailworks/mgmt"
)

// tw release (ADR-041 §8): tw-mgmt's release catalog.
//
//	tw release ls
//	tw release import <tarball>…   (SHA256SUMS next to them)

const releaseUsage = "release ls | release import <tw-<v>-<os>-<arch>.tar.gz>…"

func cmdRelease(args []string) error {
	f := newFlags("release")
	pos, err := f.parse(args)
	if err != nil {
		return err
	}
	if len(pos) == 0 {
		return fail("bad_request", "usage: tw "+releaseUsage)
	}
	s, err := openState(*f.stateDir)
	if err != nil {
		return err
	}
	switch normalizeVerb(pos[0]) {
	case "ls":
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var cat struct {
			Current string              `json:"current"`
			Items   []mgmt.ReleaseEntry `json:"items"`
		}
		if err := s.mgmt().DoHeaders(ctx, "GET", "/v1/releases", nil, &cat, map[string]string{"Tw-Actor": actor()}); err != nil {
			return err
		}
		emit(f, cat, func() {
			fmt.Printf("tw-mgmt runs %s\n", cat.Current)
			for _, e := range cat.Items {
				extra := ""
				if len(e.ContractMigrations) > 0 {
					extra += " · contract migrations"
				}
				if len(e.Artifacts) > 0 {
					extra += " · " + fmt.Sprint(len(e.Artifacts)) + " artifacts"
				}
				fmt.Printf("  %-10s %s  %s · workerd %s%s\n", e.Version, e.Date, e.Source, e.Workerd, extra)
			}
		})
		return nil
	case "import":
		if len(pos) < 2 {
			return fail("bad_request", "tw release import needs at least one release tarball", "usage: tw "+releaseUsage)
		}
		return releaseImport(f, s, pos[1:])
	}
	return fail("bad_request", fmt.Sprintf("unknown release subcommand %q", pos[0]), "usage: tw "+releaseUsage)
}

type importResult struct {
	Versions  []string `json:"versions"`
	Artifacts []string `json:"artifacts"`
}

func releaseImport(f *flags, s *state, files []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	res := importResult{Versions: []string{}, Artifacts: []string{}}
	hdr := map[string]string{"Tw-Actor": actor()}
	sumsDone := map[string]bool{}
	for _, file := range files {
		sums, err := os.ReadFile(filepath.Join(filepath.Dir(file), "SHA256SUMS"))
		if err != nil {
			return fail("bad_request", "no SHA256SUMS next to "+file)
		}
		if err := checkSum(file, sums); err != nil {
			return err
		}
		cat, ver, err := tarballCatalog(file)
		if err != nil {
			return err
		}
		if err := s.mgmt().DoHeaders(ctx, "POST", "/v1/releases", map[string]any{"entries": cat}, nil, hdr); err != nil {
			return err
		}
		if !sumsDone[ver] {
			if err := putArtifact(ctx, s, ver, "SHA256SUMS", bytes.NewReader(sums)); err != nil {
				return err
			}
			sumsDone[ver] = true
		}
		fh, err := os.Open(file)
		if err != nil {
			return err
		}
		err = putArtifact(ctx, s, ver, filepath.Base(file), fh)
		fh.Close()
		if err != nil {
			return err
		}
		res.Versions = appendUnique(res.Versions, ver)
		res.Artifacts = append(res.Artifacts, filepath.Base(file))
	}
	emit(f, res, func() {
		fmt.Printf("✓ imported tw %s (%s)\n", strings.Join(res.Versions, ", "), strings.Join(res.Artifacts, ", "))
	})
	return nil
}

func appendUnique(xs []string, x string) []string {
	for _, y := range xs {
		if y == x {
			return xs
		}
	}
	return append(xs, x)
}

// tarballCatalog reads share/tailworks/catalog.json and the version from a
// release tarball.
func tarballCatalog(file string) ([]catalog.Entry, string, error) {
	fh, err := os.Open(file)
	if err != nil {
		return nil, "", err
	}
	defer fh.Close()
	zr, err := gzip.NewReader(fh)
	if err != nil {
		return nil, "", fail("bad_request", file+": "+err.Error())
	}
	tr := tar.NewReader(zr)
	var cat []catalog.Entry
	ver := ""
	for {
		hd, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, "", err
		}
		switch {
		case strings.HasSuffix(hd.Name, "/share/tailworks/catalog.json"):
			b, _ := io.ReadAll(io.LimitReader(tr, 16<<20))
			if cat, err = catalog.Parse(b); err != nil {
				return nil, "", fail("bad_request", file+": catalog.json: "+err.Error())
			}
		case strings.HasSuffix(hd.Name, "/VERSION") && strings.Count(hd.Name, "/") == 1:
			b, _ := io.ReadAll(io.LimitReader(tr, 256))
			ver = strings.TrimSpace(string(b))
		}
	}
	if ver == "" || len(cat) == 0 {
		return nil, "", fail("bad_request", file+" has no VERSION or share/tailworks/catalog.json (a release from before the catalog can't be imported)")
	}
	return cat, ver, nil
}

func putArtifact(ctx context.Context, s *state, ver, name string, body io.Reader) error {
	c := s.mgmt()
	req, err := http.NewRequestWithContext(ctx, "PUT", c.Base+"/v1/releases/"+ver+"/artifacts/"+name, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Tw-Actor", actor())
	resp, err := c.Raw(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fail("bad_request", fmt.Sprintf("upload %s: %s: %s", name, resp.Status, strings.TrimSpace(string(b))))
	}
	return nil
}
