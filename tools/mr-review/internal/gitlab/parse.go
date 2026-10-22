package gitlab

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// MRRef is a resolved merge request reference.
type MRRef struct {
	Project string // full path ("group/sub/proj")
	IID     int
	Host    string // gitlab host ("" = let glab infer)
}

var (
	reMRURL = regexp.MustCompile(`^https?://([^/]+)/(.+)/-/merge_requests/(\d+)`)
	reIID   = regexp.MustCompile(`^\d+$`)
)

// ParseMRRef resolves an MR URL ("https://host/g/p/-/merge_requests/12") or a
// numeric IID (project+host derived from the origin remote of cwd).
func ParseMRRef(target, cwd string) (*MRRef, error) {
	if m := reMRURL.FindStringSubmatch(target); m != nil {
		iid, err := strconv.Atoi(m[3])
		if err != nil {
			return nil, fmt.Errorf("bad MR url: %w", err)
		}
		return &MRRef{Project: strings.TrimSuffix(m[2], ".git"), IID: iid, Host: m[1]}, nil
	}
	if reIID.MatchString(target) {
		iid, _ := strconv.Atoi(target)
		proj, host, err := remoteProject(cwd)
		if err != nil {
			return nil, fmt.Errorf("MR number %s but cannot resolve project from origin remote: %w", target, err)
		}
		return &MRRef{Project: proj, IID: iid, Host: host}, nil
	}
	return nil, fmt.Errorf("not an MR URL or numeric IID: %q", target)
}

// remoteProject extracts "group/project" + host from the origin remote URL
// (https, ssh and ssh:// forms).
func remoteProject(cwd string) (string, string, error) {
	out, err := exec.Command("git", "-C", cwd, "remote", "get-url", "origin").Output()
	if err != nil {
		return "", "", err
	}
	u := strings.TrimSpace(string(out))
	u = strings.TrimSuffix(u, ".git")
	switch {
	case strings.HasPrefix(u, "https://"), strings.HasPrefix(u, "http://"):
		rest := u[strings.Index(u, "://")+3:]
		host, path := rest[:strings.Index(rest, "/")], rest[strings.Index(rest, "/")+1:]
		return path, host, nil
	case strings.HasPrefix(u, "git@"):
		rest := strings.TrimPrefix(u, "git@")
		i := strings.Index(rest, ":")
		if i < 0 {
			return "", "", fmt.Errorf("unrecognized ssh remote %q", u)
		}
		return rest[i+1:], rest[:i], nil
	case strings.HasPrefix(u, "ssh://"):
		rest := strings.TrimPrefix(u, "ssh://")
		rest = strings.TrimPrefix(rest, "git@")
		host := rest[:strings.Index(rest, "/")]
		return rest[strings.Index(rest, "/")+1:], host, nil
	}
	return "", "", fmt.Errorf("unrecognized remote URL %q", u)
}

// HostFromRemote returns the inferred GitLab host for cwd ("" when unknown).
func HostFromRemote(cwd string) string {
	_, host, err := remoteProject(cwd)
	if err != nil {
		return ""
	}
	return host
}

// FindMRByRef lists open MRs and matches by IID (used by the picker).
func FindMRByRef(items []MRListItem, iid int) *MRListItem {
	for i := range items {
		if items[i].IID == iid {
			return &items[i]
		}
	}
	return nil
}
