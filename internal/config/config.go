// Package config implements docsync's layered configuration and context
// resolution. Every resolved value carries the layer it came from so that
// `docsync context` can explain surprising answers.
package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/bolacha/docsync/internal/docrules"
	"github.com/bolacha/docsync/internal/gitx"
	"gopkg.in/yaml.v3"
)

const (
	DefaultRemote = "git@github.com:your-org/docs-store.git"
	DefaultBranch = "main"
)

type ForgeConfig struct {
	Type   string `yaml:"type,omitempty" json:"type,omitempty"`
	Token  string `yaml:"token,omitempty" json:"token,omitempty"`
	APIURL string `yaml:"api_url,omitempty" json:"api_url,omitempty"`
}

type UserDefaults struct {
	Name        string `yaml:"name,omitempty" json:"name,omitempty"`
	Email       string `yaml:"email,omitempty" json:"email,omitempty"`
	DefaultTeam string `yaml:"default_team,omitempty" json:"default_team,omitempty"`
}

type ChangesetConfig struct {
	BranchPrefix string `yaml:"branch_prefix,omitempty" json:"branch_prefix,omitempty"`
	AutoCreate   *bool  `yaml:"auto_create,omitempty" json:"auto_create,omitempty"`
}

type MCPConfig struct {
	OrgWrite    string `yaml:"org_write,omitempty" json:"org_write,omitempty"`
	MaxFileSize int64  `yaml:"max_file_size,omitempty" json:"max_file_size,omitempty"`
}

// UserConfig is the personal config at ~/.config/docsync/config.yaml.
type UserConfig struct {
	Remote     string          `yaml:"remote,omitempty" json:"remote,omitempty"`
	Forge      ForgeConfig     `yaml:"forge,omitempty" json:"forge,omitempty"`
	User       UserDefaults    `yaml:"user,omitempty" json:"user,omitempty"`
	Machine    string          `yaml:"machine,omitempty" json:"machine,omitempty"`
	Changesets ChangesetConfig `yaml:"changesets,omitempty" json:"changesets,omitempty"`
	MCP        MCPConfig       `yaml:"mcp,omitempty" json:"mcp,omitempty"`

	// Optional convenience fields, also settable per repository.
	Project   string   `yaml:"project,omitempty" json:"project,omitempty"`
	Team      string   `yaml:"team,omitempty" json:"team,omitempty"`
	Domain    string   `yaml:"domain,omitempty" json:"domain,omitempty"`
	Languages []string `yaml:"languages,omitempty" json:"languages,omitempty"`
}

// RepoConfig is the committed .docsync.yaml in a code repository.
type RepoConfig struct {
	Project   string     `yaml:"project,omitempty" json:"project,omitempty"`
	Team      string     `yaml:"team,omitempty" json:"team,omitempty"`
	Domain    string     `yaml:"domain,omitempty" json:"domain,omitempty"`
	Languages []string   `yaml:"languages,omitempty" json:"languages,omitempty"`
	Docs      DocsConfig `yaml:"docs,omitempty" json:"docs,omitempty"`
}

type DocsConfig struct {
	Required []string `yaml:"required,omitempty" json:"required,omitempty"`
}

type Team struct {
	Name       string `yaml:"name,omitempty" json:"name,omitempty"`
	GitHubTeam string `yaml:"github_team,omitempty" json:"github_team,omitempty"`
	Domain     string `yaml:"domain,omitempty" json:"domain,omitempty"`
}

type RepoMapping struct {
	Team   string `yaml:"team,omitempty" json:"team,omitempty"`
	Domain string `yaml:"domain,omitempty" json:"domain,omitempty"`
}

// Registry is the org-wide mapping file in docs-store/.docsync/registry.yaml.
type Registry struct {
	Teams     map[string]Team        `yaml:"teams,omitempty" json:"teams,omitempty"`
	Repos     map[string]RepoMapping `yaml:"repos,omitempty" json:"repos,omitempty"`
	Aliases   map[string]string      `yaml:"aliases,omitempty" json:"aliases,omitempty"`
	Defaults  RegistryDefaults       `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	Reviewers map[string]string      `yaml:"reviewers,omitempty" json:"reviewers,omitempty"`
	Extra     map[string]interface{} `yaml:",inline" json:"-"`
}

type RegistryDefaults struct {
	LanguagesHint []string `yaml:"languages_hint,omitempty" json:"languages_hint,omitempty"`
}

// Flags are the highest-priority context values.
type Flags struct {
	Project   string
	Team      string
	Domain    string
	Languages []string
}

// Context is the resolved answer to "who/where am I".
type Context struct {
	UserName  string   `json:"user_name,omitempty"`
	UserEmail string   `json:"user_email,omitempty"`
	Machine   string   `json:"machine,omitempty"`
	Project   string   `json:"project,omitempty"`
	Team      string   `json:"team,omitempty"`
	Domain    string   `json:"domain,omitempty"`
	Languages []string `json:"languages,omitempty"`
	Area      string   `json:"area,omitempty"`
	Reviewers string   `json:"reviewers,omitempty"`

	Sources    map[string]string `json:"sources"`
	Unresolved []string          `json:"unresolved,omitempty"`
	Warnings   []string          `json:"warnings,omitempty"`

	Detected gitx.Detected `json:"-"`
	RepoRoot string        `json:"repo_root,omitempty"`
}

// BranchUser returns a stable branch-safe user token.
func (c *Context) BranchUser() string {
	if c == nil {
		return "user"
	}
	if c.UserName != "" {
		return gitx.Slug(c.UserName)
	}
	if c.UserEmail != "" {
		if i := strings.Index(c.UserEmail, "@"); i > 0 {
			return gitx.Slug(c.UserEmail[:i])
		}
	}
	return "user"
}

// Home returns the docsync state directory.
func Home() (string, error) {
	if v := strings.TrimSpace(os.Getenv("DOCSYNC_HOME")); v != "" {
		return filepath.Abs(v)
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".docsync"), nil
}

// UserConfigPath returns the per-user config path.
func UserConfigPath() (string, error) {
	if v := strings.TrimSpace(os.Getenv("DOCSYNC_CONFIG")); v != "" {
		return filepath.Abs(v)
	}
	if runtime.GOOS == "windows" {
		if dir := os.Getenv("APPDATA"); dir != "" {
			return filepath.Join(dir, "docsync", "config.yaml"), nil
		}
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "docsync", "config.yaml"), nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".config", "docsync", "config.yaml"), nil
}

func normalizeUserConfig(c *UserConfig) {
	if c.Forge.Type == "" {
		c.Forge.Type = "github"
	}
	if c.Changesets.BranchPrefix == "" {
		c.Changesets.BranchPrefix = "docs"
	}
	if c.Changesets.AutoCreate == nil {
		v := true
		c.Changesets.AutoCreate = &v
	}
	if c.MCP.OrgWrite == "" {
		c.MCP.OrgWrite = "propose-only"
	}
	if c.MCP.MaxFileSize <= 0 {
		c.MCP.MaxFileSize = 1 << 20
	}
}

// LoadUser loads (or creates defaults for) the user config.
func LoadUser() (UserConfig, error) {
	var c UserConfig
	p, err := UserConfigPath()
	if err != nil {
		return c, err
	}
	b, err := os.ReadFile(p)
	switch {
	case errors.Is(err, os.ErrNotExist):
		// A missing config file is normal; environment overrides still apply.
	case err != nil:
		return c, err
	default:
		if err := yaml.Unmarshal(b, &c); err != nil {
			return c, fmt.Errorf("parse %s: %w", p, err)
		}
	}
	normalizeUserConfig(&c)
	if v := strings.TrimSpace(os.Getenv("DOCSYNC_REMOTE")); v != "" {
		c.Remote = v
	}
	return c, nil
}

// SaveUser writes the user config atomically with restrictive permissions.
func SaveUser(c UserConfig) error {
	normalizeUserConfig(&c)
	p, err := UserConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := yaml.Marshal(&c)
	if err != nil {
		return err
	}
	return atomicWrite(p, b, 0o600)
}

// LoadRepo loads .docsync.yaml from a code repository root.
func LoadRepo(root string) (RepoConfig, error) {
	var c RepoConfig
	if root == "" {
		return c, nil
	}
	b, err := os.ReadFile(filepath.Join(root, ".docsync.yaml"))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := yaml.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("parse .docsync.yaml: %w", err)
	}
	return c, nil
}

func SaveRepo(root string, c RepoConfig) error {
	if root == "" {
		return errors.New("not inside a git repository")
	}
	b, err := yaml.Marshal(&c)
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(root, ".docsync.yaml"), b, 0o644)
}

func LoadRegistry(storeRoot string) (Registry, error) {
	var r Registry
	if storeRoot == "" {
		return r, nil
	}
	p := filepath.Join(storeRoot, ".docsync", "registry.yaml")
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	if err := yaml.Unmarshal(b, &r); err != nil {
		return r, fmt.Errorf("parse registry: %w", err)
	}
	return r, nil
}

func atomicWrite(p string, b []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(p), ".docsync-tmp-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, p)
}

// ResolveInput is everything the resolver may consult.
type ResolveInput struct {
	Cwd      string
	Flags    Flags
	User     UserConfig
	Repo     RepoConfig
	Registry Registry
	Store    string // local mirror root, for CODEOWNERS
	Env      map[string]string
}

// Resolve applies the documented precedence and records provenance.
func Resolve(ctx context.Context, in ResolveInput) *Context {
	if in.Env == nil {
		in.Env = envMap()
	}
	c := &Context{Sources: map[string]string{}}
	d := gitx.Detect(ctx, in.Cwd)
	c.Detected = d
	c.RepoRoot = d.Root

	// Identity.
	switch {
	case strings.TrimSpace(in.User.User.Name) != "":
		c.UserName, c.Sources["user"] = in.User.User.Name, "user config"
	case strings.TrimSpace(in.Env["DOCSYNC_USER_NAME"]) != "":
		c.UserName, c.Sources["user"] = in.Env["DOCSYNC_USER_NAME"], "environment"
	case d.Name != "":
		c.UserName, c.Sources["user"] = d.Name, "git config"
	case strings.TrimSpace(in.Env["USER"]) != "":
		c.UserName, c.Sources["user"] = in.Env["USER"], "OS user"
	case strings.TrimSpace(in.Env["USERNAME"]) != "":
		c.UserName, c.Sources["user"] = in.Env["USERNAME"], "OS user"
	}
	switch {
	case strings.TrimSpace(in.User.User.Email) != "":
		c.UserEmail, c.Sources["email"] = in.User.User.Email, "user config"
	case strings.TrimSpace(in.Env["DOCSYNC_USER_EMAIL"]) != "":
		c.UserEmail, c.Sources["email"] = in.Env["DOCSYNC_USER_EMAIL"], "environment"
	case d.Email != "":
		c.UserEmail, c.Sources["email"] = d.Email, "git config"
	}
	if d.Root == "" {
		c.Warnings = append(c.Warnings, "not inside a git repository; project detection is limited")
	}

	// Machine.
	switch {
	case in.User.Machine == "none":
		c.Machine, c.Sources["machine"] = "", "user config (omitted)"
	case strings.TrimSpace(in.User.Machine) != "":
		c.Machine, c.Sources["machine"] = sanitizeMachine(in.User.Machine), "user config"
	default:
		h, _ := os.Hostname()
		c.Machine, c.Sources["machine"] = sanitizeMachine(h), "hostname"
	}

	// Project: flag -> env -> repo config -> user config -> alias -> normalized remote -> local/<dir>.
	c.Project, c.Sources["project"] = firstNonEmpty(
		val(in.Flags.Project, "flag"),
		val(in.Env["DOCSYNC_PROJECT"], "environment"),
		val(in.Repo.Project, ".docsync.yaml"),
		val(in.User.Project, "user config"),
	)
	if c.Project == "" && d.Project != "" {
		if alias, ok := in.Registry.Aliases[d.Project]; ok && alias != "" {
			c.Project, c.Sources["project"] = alias, "registry alias"
		} else {
			c.Project, c.Sources["project"] = d.Project, "git remote origin"
		}
	}
	if c.Project == "" && d.Root != "" {
		c.Project, c.Sources["project"] = "local/"+gitx.Slug(gitx.RepoName(d.Root)), "local path (weak)"
		c.Warnings = append(c.Warnings, "project was inferred from the directory name; set project in .docsync.yaml for stable docs")
	}
	if c.Project == "" {
		c.Unresolved = append(c.Unresolved, "project")
	} else if err := docrules.ValidateProject(c.Project); err != nil {
		c.Warnings = append(c.Warnings, err.Error()+"; project-scoped writes will fail until it is fixed")
	}

	// Registry repo mapping.
	mapping, hasMapping := in.Registry.Mapping(c.Project)

	// Team: flag -> env -> repo -> user -> registry -> code-repo CODEOWNERS ->
	// store CODEOWNERS -> user default.
	c.Team, c.Sources["team"] = firstNonEmpty(
		val(in.Flags.Team, "flag"),
		val(in.Env["DOCSYNC_TEAM"], "environment"),
		val(in.Repo.Team, ".docsync.yaml"),
		val(in.User.Team, "user config"),
	)
	if c.Team == "" && hasMapping && mapping.Team != "" {
		c.Team, c.Sources["team"] = mapping.Team, "registry repos mapping"
	}
	if c.Team == "" && d.Root != "" {
		if t := rootTeam(d.Root); t != "" {
			c.Team, c.Sources["team"] = t, "code repo CODEOWNERS root"
		}
	}
	if c.Team == "" {
		if t := rootTeam(in.Store); t != "" {
			c.Team, c.Sources["team"] = t, "store CODEOWNERS root"
		}
	}
	if c.Team == "" && in.User.User.DefaultTeam != "" {
		c.Team, c.Sources["team"] = in.User.User.DefaultTeam, "user default_team"
	}
	if c.Team == "" {
		c.Unresolved = append(c.Unresolved, "team")
	}

	// Domain.
	c.Domain, c.Sources["domain"] = firstNonEmpty(
		val(in.Flags.Domain, "flag"),
		val(in.Env["DOCSYNC_DOMAIN"], "environment"),
		val(in.Repo.Domain, ".docsync.yaml"),
		val(in.User.Domain, "user config"),
		val(mapping.Domain, "registry repos mapping"),
	)

	// Languages: flag -> env -> repo -> user -> manifests -> registry defaults.
	if len(in.Flags.Languages) > 0 {
		c.Languages, c.Sources["languages"] = cleanLanguages(in.Flags.Languages), "flag"
	} else if v := splitCSV(in.Env["DOCSYNC_LANGUAGES"]); len(v) > 0 {
		c.Languages, c.Sources["languages"] = cleanLanguages(v), "environment"
	} else if len(in.Repo.Languages) > 0 {
		c.Languages, c.Sources["languages"] = cleanLanguages(in.Repo.Languages), ".docsync.yaml"
	} else if len(in.User.Languages) > 0 {
		c.Languages, c.Sources["languages"] = cleanLanguages(in.User.Languages), "user config"
	}
	if len(c.Languages) == 0 && d.Root != "" {
		if langs := detectLanguages(d.Root); len(langs) > 0 {
			c.Languages, c.Sources["languages"] = langs, "detected manifests"
		}
	}
	if len(c.Languages) == 0 && len(in.Registry.Defaults.LanguagesHint) > 0 {
		c.Languages, c.Sources["languages"] = cleanLanguages(in.Registry.Defaults.LanguagesHint), "registry languages_hint"
	}
	if len(c.Languages) == 0 {
		c.Sources["languages"] = "not detected"
	}

	codeownersReviewers := ReviewersFor(in.Store, c.ProjectArea())
	if r := in.Registry.Reviewers[c.Project]; r != "" {
		c.Reviewers, c.Sources["reviewers"] = r, "registry reviewers"
	} else if codeownersReviewers != "" {
		c.Reviewers, c.Sources["reviewers"] = codeownersReviewers, "store CODEOWNERS"
	}

	return c
}

// ProjectArea returns projects/<project>, or "" when project is unresolved.
func (c *Context) ProjectArea() string {
	if c == nil || c.Project == "" {
		return ""
	}
	return "projects/" + strings.Trim(c.Project, "/")
}

// TeamArea returns teams/<team>.
func (c *Context) TeamArea() string {
	if c == nil || c.Team == "" {
		return ""
	}
	return "teams/" + strings.Trim(c.Team, "/")
}

// OrgArea is the org-wide area.
func (c *Context) OrgArea() string { return "org" }

type candidate struct {
	value  string
	source string
}

func val(v, source string) candidate {
	if strings.TrimSpace(v) == "" {
		return candidate{}
	}
	return candidate{value: strings.TrimSpace(v), source: source}
}

func firstNonEmpty(vals ...candidate) (string, string) {
	for _, v := range vals {
		if v.value != "" {
			return v.value, v.source
		}
	}
	return "", ""
}

func cleanLanguages(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range in {
		l = strings.ToLower(strings.TrimSpace(l))
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
	}
	sort.Strings(out)
	return out
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func sanitizeMachine(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	s = gitx.Slug(s)
	if s == "" || s == "untitled" {
		return "unknown"
	}
	return s
}

func envMap() map[string]string {
	m := map[string]string{}
	for _, k := range []string{
		"DOCSYNC_PROJECT", "DOCSYNC_TEAM", "DOCSYNC_DOMAIN", "DOCSYNC_LANGUAGES",
		"DOCSYNC_REMOTE", "DOCSYNC_USER_NAME", "DOCSYNC_USER_EMAIL",
		"USER", "USERNAME",
	} {
		if v := os.Getenv(k); v != "" {
			m[k] = v
		}
	}
	return m
}

func detectLanguages(root string) []string {
	type manifest struct {
		file string
		lang string
	}
	manifests := []manifest{
		{"go.mod", "go"},
		{"package.json", "node"},
		{"composer.json", "php"},
		{"pom.xml", "java"},
		{"build.gradle", "java"},
		{"build.gradle.kts", "java"},
		{"pyproject.toml", "python"},
		{"requirements.txt", "python"},
		{"Gemfile", "ruby"},
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range manifests {
		if _, err := os.Stat(filepath.Join(root, m.file)); err == nil && !seen[m.lang] {
			seen[m.lang] = true
			out = append(out, m.lang)
		}
	}
	sort.Strings(out)
	return out
}

// Mapping finds the most specific repo mapping. Exact matches win, then the
// longest glob pattern.
func (r Registry) Mapping(project string) (RepoMapping, bool) {
	if project == "" {
		return RepoMapping{}, false
	}
	if m, ok := r.Repos[project]; ok {
		return m, true
	}
	keys := make([]string, 0, len(r.Repos))
	for k := range r.Repos {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, k := range keys {
		if ok, _ := path.Match(k, project); ok {
			return r.Repos[k], true
		}
	}
	return RepoMapping{}, false
}

// SetRepoMapping updates one repository mapping while preserving existing
// fields the caller does not change. Keeping the merge here prevents registry
// writers from dropping a previously configured team or domain.
func (r *Registry) SetRepoMapping(project string, update func(*RepoMapping)) {
	if project == "" || update == nil {
		return
	}
	if r.Repos == nil {
		r.Repos = map[string]RepoMapping{}
	}
	mapping := r.Repos[project]
	update(&mapping)
	r.Repos[project] = mapping
}

// CodeOwnerRule is one useful line from .github/CODEOWNERS.
type CodeOwnerRule struct {
	Pattern string
	Owners  []string
}

func LoadCodeOwners(storeRoot string) []CodeOwnerRule {
	if storeRoot == "" {
		return nil
	}
	var b []byte
	var err error
	for _, rel := range []string{".github/CODEOWNERS", "CODEOWNERS", "docs/CODEOWNERS"} {
		b, err = os.ReadFile(filepath.Join(storeRoot, rel))
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil
		}
	}
	if err != nil {
		return nil
	}
	var rules []CodeOwnerRule
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		rules = append(rules, CodeOwnerRule{Pattern: fields[0], Owners: fields[1:]})
	}
	return rules
}

// ReviewersFor finds the last matching CODEOWNERS rule for an area path.
func ReviewersFor(storeRoot, area string) string {
	if area == "" {
		return ""
	}
	area = strings.Trim(area, "/")
	var owners []string
	for _, r := range LoadCodeOwners(storeRoot) {
		if codeOwnersMatch(r.Pattern, area) {
			owners = r.Owners
		}
	}
	return strings.Join(owners, " ")
}

func rootTeam(storeRoot string) string {
	for _, r := range LoadCodeOwners(storeRoot) {
		p := strings.Trim(strings.TrimSpace(r.Pattern), "/")
		if p != "" && p != "*" && p != "**" {
			continue
		}
		if len(r.Owners) == 0 {
			continue
		}
		owner := strings.TrimPrefix(r.Owners[0], "@")
		if i := strings.LastIndex(owner, "/"); i >= 0 {
			owner = owner[i+1:]
		}
		return gitx.Slug(owner)
	}
	return ""
}

func codeOwnersMatch(pattern, area string) bool {
	p := strings.TrimSpace(pattern)
	if p == "" {
		return false
	}
	p = strings.TrimPrefix(p, "/")
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		p = "*"
	}
	if strings.HasSuffix(p, "/**") {
		p = strings.TrimSuffix(p, "/**")
	}
	candidates := []string{area, area + "/README.md"}
	for _, c := range candidates {
		if ok, _ := path.Match(p, c); ok {
			return true
		}
		if !strings.Contains(p, "/") {
			parts := strings.Split(c, "/")
			if ok, _ := path.Match(p, parts[len(parts)-1]); ok {
				return true
			}
			if ok, _ := path.Match("**/"+p, c); ok {
				return true
			}
		}
		if strings.HasPrefix(c, p+"/") {
			return true
		}
	}
	return false
}

// GetUserKey returns a known user config key.
func GetUserKey(c UserConfig, key string) (string, error) {
	switch key {
	case "remote":
		return c.Remote, nil
	case "forge.type":
		return c.Forge.Type, nil
	case "forge.token":
		if c.Forge.Token == "" {
			return "", nil
		}
		return "***", nil
	case "forge.api_url":
		return c.Forge.APIURL, nil
	case "user.name":
		return c.User.Name, nil
	case "user.email":
		return c.User.Email, nil
	case "user.default_team":
		return c.User.DefaultTeam, nil
	case "machine":
		return c.Machine, nil
	case "changesets.branch_prefix":
		return c.Changesets.BranchPrefix, nil
	case "changesets.auto_create":
		if c.Changesets.AutoCreate == nil {
			return "true", nil
		}
		return strconv.FormatBool(*c.Changesets.AutoCreate), nil
	case "mcp.org_write":
		return c.MCP.OrgWrite, nil
	case "mcp.max_file_size":
		return strconv.FormatInt(c.MCP.MaxFileSize, 10), nil
	case "project":
		return c.Project, nil
	case "team":
		return c.Team, nil
	case "domain":
		return c.Domain, nil
	case "languages":
		return strings.Join(c.Languages, ","), nil
	default:
		return "", fmt.Errorf("unknown user config key %q", key)
	}
}

// GetRepoKey returns a known repo config key.
func GetRepoKey(c RepoConfig, key string) (string, error) {
	switch key {
	case "project":
		return c.Project, nil
	case "team":
		return c.Team, nil
	case "domain":
		return c.Domain, nil
	case "languages":
		return strings.Join(c.Languages, ","), nil
	case "docs.required":
		return strings.Join(c.Docs.Required, ","), nil
	default:
		return "", fmt.Errorf("unknown repo config key %q", key)
	}
}

// SetUserKey updates one known user config key. It is intentionally explicit;
// typo'd keys are errors instead of silently creating unknown config.
func SetUserKey(c *UserConfig, key, value string) error {
	switch key {
	case "remote":
		c.Remote = value
	case "forge.type":
		c.Forge.Type = value
	case "forge.token":
		c.Forge.Token = value
	case "forge.api_url":
		c.Forge.APIURL = value
	case "user.name":
		c.User.Name = value
	case "user.email":
		c.User.Email = value
	case "user.default_team":
		c.User.DefaultTeam = value
	case "machine":
		c.Machine = value
	case "changesets.branch_prefix":
		c.Changesets.BranchPrefix = gitx.Slug(value)
	case "changesets.auto_create":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("changesets.auto_create must be true or false")
		}
		c.Changesets.AutoCreate = &b
	case "mcp.org_write":
		if value != "propose-only" && value != "disabled" {
			return fmt.Errorf("mcp.org_write must be propose-only or disabled")
		}
		c.MCP.OrgWrite = value
	case "mcp.max_file_size":
		n, err := parseByteSize(value)
		if err != nil {
			return err
		}
		c.MCP.MaxFileSize = n
	case "project":
		project, err := validateProjectField(value)
		if err != nil {
			return err
		}
		c.Project = project
	case "team":
		c.Team = value
	case "domain":
		c.Domain = value
	case "languages":
		c.Languages = cleanLanguages(splitCSV(value))
	default:
		return fmt.Errorf("unknown user config key %q", key)
	}
	normalizeUserConfig(c)
	return nil
}

// SetRepoKey updates one known repo config key.
func SetRepoKey(c *RepoConfig, key, value string) error {
	switch key {
	case "project":
		project, err := validateProjectField(value)
		if err != nil {
			return err
		}
		c.Project = project
	case "team":
		c.Team = value
	case "domain":
		c.Domain = value
	case "languages":
		c.Languages = cleanLanguages(splitCSV(value))
	case "docs.required":
		c.Docs.Required = splitCSV(value)
	default:
		return fmt.Errorf("unknown repo config key %q", key)
	}
	return nil
}

func validateProjectField(value string) (string, error) {
	value = strings.Trim(strings.TrimSpace(value), "/")
	if value == "" {
		return "", nil
	}
	if err := docrules.ValidateProject(value); err != nil {
		return "", err
	}
	return value, nil
}

func parseByteSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	mult := int64(1)
	for suffix, m := range map[string]int64{"kb": 1 << 10, "mb": 1 << 20, "gb": 1 << 30} {
		if strings.HasSuffix(s, suffix) {
			mult = m
			s = strings.TrimSpace(strings.TrimSuffix(s, suffix))
			break
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	return n * mult, nil
}
