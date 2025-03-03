package version

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime/debug"
	"strings"
	"time"
)

const (
	gocqlPackage     = "github.com/gocql/gocql"
	githubTimeout    = 5 * time.Second
	userAgent        = "scylla-bench (github.com/scylladb/scylla-bench)"
	githubAPIBaseURL = "https://api.github.com"
)

// Default version values; can be overridden via ldflags
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

type ComponentInfo struct {
	Version    string `json:"version"`
	CommitDate string `json:"commit_date"`
	CommitSHA  string `json:"commit_sha"`
}

// nolint
type VersionInfo struct {
	ScyllaBench ComponentInfo `json:"scylla-bench"`
	Driver      ComponentInfo `json:"scylla-driver"`
}

type githubRelease struct {
	CreatedAt time.Time `json:"created_at"`
	TagName   string    `json:"tag_name"`
}

type githubTag struct {
	Object struct {
		SHA string `json:"sha"`
	} `json:"object"`
}

type githubClient struct {
	client    *http.Client
	baseURL   string
	userAgent string
}

func newGithubClient() *githubClient {
	return &githubClient{
		client:    &http.Client{Timeout: githubTimeout},
		baseURL:   githubAPIBaseURL,
		userAgent: userAgent,
	}
}

// Performs an HTTP GET request and decodes the JSON response to the target
func (g *githubClient) getJSON(path string, target interface{}) error {
	url := g.baseURL + path
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", g.userAgent)

	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	if err = json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("failed to decode JSON: %w", err)
	}
	return nil
}

// Fetches release date and commit SHA for the given version
func (g *githubClient) getReleaseInfo(owner, repo, version string) (date, sha string, err error) {
	// Fetch releases
	path := fmt.Sprintf("/repos/%s/%s/releases", owner, repo)
	var releases []githubRelease
	if err = g.getJSON(path, &releases); err != nil {
		return "", "", fmt.Errorf("failed to fetch releases: %w", err)
	}

	// Find matching release
	cleanVersion := strings.TrimPrefix(version, "v")
	var releaseDate, tagName string
	for _, release := range releases {
		if strings.TrimPrefix(release.TagName, "v") == cleanVersion {
			releaseDate, tagName = release.CreatedAt.Format(time.RFC3339), release.TagName
			break
		}
	}
	if tagName == "" {
		return "", "", fmt.Errorf("release %s not found", version)
	}

	// Fetch tag info to get the commit SHA
	tagPath := fmt.Sprintf("/repos/%s/%s/git/refs/tags/%s", owner, repo, tagName)
	var tag githubTag
	if err = g.getJSON(tagPath, &tag); err != nil {
		return releaseDate, "", fmt.Errorf("failed to fetch tag info: %w", err)
	}

	return releaseDate, tag.Object.SHA, nil
}

// Fetches information about the given commit
func (g *githubClient) getCommitInfo(owner, repo, sha string) (date string, err error) {
	path := fmt.Sprintf("/repos/%s/%s/commits/%s", owner, repo, sha)
	var commit struct {
		Commit struct {
			Committer struct {
				Date string `json:"date"`
			} `json:"committer"`
		} `json:"commit"`
	}

	if err = g.getJSON(path, &commit); err != nil {
		return "", fmt.Errorf("failed to fetch commit info: %w", err)
	}

	return commit.Commit.Committer.Date, nil
}

func tryGitCommand(args ...string) (string, bool) {
	output, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(output)), true
}

// Reads version info from local Git repository
func getGitVersionInfo() (ver, sha, buildDate string, ok bool) {
	// Check if git is available and we're in a git repo
	if _, ok = tryGitCommand("rev-parse", "--is-inside-work-tree"); !ok {
		return "", "", "", false
	}

	// Get released version details
	sha, ok = tryGitCommand("rev-parse", "HEAD")
	if !ok {
		return "", "", "", false
	}
	buildDate, ok = tryGitCommand("log", "-1", "--format=%cd", "--date=format:%Y-%m-%dT%H:%M:%SZ")
	if !ok {
		return "", "", "", false
	}
	//nolint
	if tags, ok := tryGitCommand("tag", "--points-at", "HEAD"); ok && tags != "" {
		for _, tag := range strings.Split(tags, "\n") {
			if strings.HasPrefix(tag, "v") {
				return tag, sha, buildDate, true
			}
		}
	}

	// If not a released version, use the most recent tag to build a dev version string
	//nolint
	if closestTag, ok := tryGitCommand("describe", "--tags", "--abbrev=0"); ok && strings.HasPrefix(closestTag, "v") {
		return fmt.Sprintf("%s-dev-%s", closestTag, sha[:8]), sha, buildDate, true
	}

	return fmt.Sprintf("dev-%s", sha[:8]), sha, buildDate, true
}

func isCommitSHA(s string) bool {
	shaPattern := regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)
	return shaPattern.MatchString(s)
}

func extractCommitSHA(version string) string {
	if isCommitSHA(version) {
		return version
	}

	// If version is in pseudo-version format (e.g v0.0.0-20230101120000-abcdef123456)
	if strings.Count(version, "-") >= 2 {
		parts := strings.Split(version, "-")
		candidate := parts[len(parts)-1]
		if isCommitSHA(candidate) {
			return candidate
		}
	}

	return ""
}

func extractRepoOwner(repoPath, defaultOwner string) string {
	parts := strings.Split(repoPath, "/")
	if len(parts) >= 2 {
		return parts[1]
	}
	return defaultOwner
}

// Extracts driver version info
func getDriverVersionInfo() ComponentInfo {
	var (
		err error
		sha string
	)

	info := ComponentInfo{
		Version:    "unknown",
		CommitSHA:  "unknown",
		CommitDate: "unknown",
	}

	// Get driver module from build info
	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		return info
	}
	var driverModule *debug.Module
	for _, dep := range buildInfo.Deps {
		if dep.Path == gocqlPackage {
			driverModule = dep
			break
		}
	}
	if driverModule == nil {
		return info
	}

	github := newGithubClient()
	if replacement := driverModule.Replace; replacement != nil {
		// If custom version is set via GOCQL_VERSION env var
		if envSHA := os.Getenv("GOCQL_VERSION"); envSHA != "" && isCommitSHA(envSHA) {
			info.Version = envSHA
			info.CommitSHA = envSHA

			repoOwner := extractRepoOwner(os.Getenv("GOCQL_REPO"), "scylladb")
			if date, err = github.getCommitInfo(repoOwner, "gocql", envSHA); err == nil {
				info.CommitDate = date
			}
			return info
		}

		// Otherwise try to extract released version (e.g. v1.2.3)
		if strings.HasPrefix(replacement.Version, "v") && !strings.Contains(replacement.Version, "-") {
			info.Version = replacement.Version
			if date, sha, err = github.getReleaseInfo("scylladb", "gocql", replacement.Version); err == nil {
				info.CommitDate = date
				info.CommitSHA = sha
			}
			return info
		}

		// Otherwise handle pseudo-versions or direct SHA
		version = replacement.Version
		if sha = extractCommitSHA(version); sha != "" {
			info.Version = sha
			info.CommitSHA = sha

			repoOwner := extractRepoOwner(replacement.Path, "scylladb")
			if date, err = github.getCommitInfo(repoOwner, "gocql", sha); err == nil {
				info.CommitDate = date
			}
			return info
		}

		// As a fallback, just use what we have as a version
		info.Version = version
		return info
	}

	// If no driver module replacement, this is the upstream gocql driver
	info.Version = driverModule.Version
	info.CommitSHA = "upstream release"
	return info
}

// Extracts build settings from debug.BuildInfo to be compatible with different Go versions, as
// in older Go versions (pre 1.18), BuildInfo doesn't have Settings field
func getBuildInfoSettings(_ *debug.BuildInfo) (map[string]string, bool) {
	settings := make(map[string]string)
	// For Go 1.17 return an empty map
	return settings, false
}

// Extracts scylla-bench version info
func getMainBuildInfo() (ver, sha, buildDate string) {
	ver, sha, buildDate = version, commit, date

	// Use git info if no version was provided via ldflags
	if ver == "dev" {
		if gitVer, gitSha, gitDate, ok := getGitVersionInfo(); ok {
			return gitVer, gitSha, gitDate
		}
	}

	// Otherwise fall back to go build info
	if info, ok := debug.ReadBuildInfo(); ok {
		if ver == "dev" {
			if info.Main.Version != "" {
				ver = info.Main.Version
			} else {
				ver = "(devel)"
			}
		}

		// Try to get VCS information from build info settings
		//nolint:govet
		if settings, ok := getBuildInfoSettings(info); ok {
			if vcsRev, ok := settings["vcs.revision"]; ok && sha == "unknown" {
				sha = vcsRev
			}
			//nolint:govet
			if vcsTime, ok := settings["vcs.time"]; ok && buildDate == "unknown" {
				buildDate = vcsTime
			}
		}
	}
	return
}

// GetVersionInfo returns the version info for scylla-bench and scylla-gocql-driver
func GetVersionInfo() VersionInfo {
	ver, sha, buildDate := getMainBuildInfo()
	return VersionInfo{
		ScyllaBench: ComponentInfo{
			Version:    ver,
			CommitDate: buildDate,
			CommitSHA:  sha,
		},
		Driver: getDriverVersionInfo(),
	}
}

// FormatHuman returns a human-readable string with version info
func (v VersionInfo) FormatHuman() string {
	return fmt.Sprintf(`scylla-bench:
    version: %s
    commit sha: %s
    commit date: %s
scylla-gocql-driver:
    version: %s
    commit sha: %s
    commit date: %s`,
		v.ScyllaBench.Version,
		v.ScyllaBench.CommitSHA,
		v.ScyllaBench.CommitDate,
		v.Driver.Version,
		v.Driver.CommitSHA,
		v.Driver.CommitDate)
}

// FormatJSON returns a JSON-formatted string with version info
func (v VersionInfo) FormatJSON() (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal version info to JSON: %w", err)
	}
	return string(data), nil
}
