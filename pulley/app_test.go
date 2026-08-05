package pulley

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandBaseDirUsesResolvedExecutablePath(t *testing.T) {
	origExec := executablePath
	origEval := evalSymlinks
	t.Cleanup(func() {
		executablePath = origExec
		evalSymlinks = origEval
	})

	executablePath = func() (string, error) {
		return "/usr/local/bin/pulley", nil
	}
	evalSymlinks = func(path string) (string, error) {
		if path != "/usr/local/bin/pulley" {
			t.Fatalf("unexpected path: %s", path)
		}
		return "/home/user/projects/pulley/pulley", nil
	}

	base, err := commandBaseDir()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if base != "/home/user/projects/pulley" {
		t.Fatalf("unexpected base dir: %s", base)
	}
}

func TestNextAvailableName(t *testing.T) {
	root := t.TempDir()
	if got := nextAvailableName(root, "proj"); got != "proj" {
		t.Fatalf("expected proj, got %s", got)
	}
	if err := os.Mkdir(filepath.Join(root, "proj"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := nextAvailableName(root, "proj"); got != "proj2" {
		t.Fatalf("expected proj2, got %s", got)
	}
	if err := os.Mkdir(filepath.Join(root, "proj2"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := nextAvailableName(root, "proj"); got != "proj3" {
		t.Fatalf("expected proj3, got %s", got)
	}
}

func TestTicketPrefixedName(t *testing.T) {
	got, err := ticketPrefixedName("123", "backend")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != "123-backend" {
		t.Fatalf("expected 123-backend, got %s", got)
	}
}

func TestTicketPrefixedNameRejectsEmptyTicketOrName(t *testing.T) {
	if _, err := ticketPrefixedName("", "backend"); err == nil {
		t.Fatal("expected error for empty ticket")
	}
	if _, err := ticketPrefixedName("123", ""); err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestTicketPrefixedNameTrimsWhitespace(t *testing.T) {
	got, err := ticketPrefixedName(" 123 ", " backend ")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != "123-backend" {
		t.Fatalf("expected trimmed value, got %s", got)
	}
}

func TestDestinationNameFromChoiceUsesAutoWhenEmpty(t *testing.T) {
	got, err := destinationNameFromChoice("1234", "1234-ec-backend", "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != "1234-ec-backend" {
		t.Fatalf("expected auto name, got %s", got)
	}
}

func TestDestinationNameFromChoicePrefixesCustomBaseName(t *testing.T) {
	got, err := destinationNameFromChoice("1234", "1234-ec-backend", "backend")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != "1234-backend" {
		t.Fatalf("expected ticket-prefixed custom name, got %s", got)
	}
}

func TestDestinationNameFromChoiceKeepsAlreadyPrefixedName(t *testing.T) {
	got, err := destinationNameFromChoice("1234", "1234-ec-backend", "1234-backend")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != "1234-backend" {
		t.Fatalf("expected unchanged prefixed name, got %s", got)
	}
}

func TestBranchNameFromChoiceUsesBaseBranchWhenEmpty(t *testing.T) {
	got, createNew, err := branchNameFromChoice("1234", "main", "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if createNew {
		t.Fatal("expected no new branch to be created")
	}
	if got != "main" {
		t.Fatalf("expected base branch main, got %s", got)
	}
}

func TestBranchNameFromChoicePrefixesCustomBranchName(t *testing.T) {
	got, createNew, err := branchNameFromChoice("1234", "main", "add-search")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !createNew {
		t.Fatal("expected a new branch to be created")
	}
	if got != "feature/1234-add-search" {
		t.Fatalf("expected everon-convention branch name, got %s", got)
	}
}

func TestEveronBranchNameAppliesConvention(t *testing.T) {
	cases := []struct {
		name   string
		ticket string
		choice string
		want   string
	}{
		{"slugifies free text", "1234", "Add Search Bar", "feature/1234-add-search-bar"},
		{"collapses punctuation", "1234", "add   search__bar!!", "feature/1234-add-search-bar"},
		{"keeps existing prefix", "1234", "feature/1234-add-search", "feature/1234-add-search"},
		{"keeps ticket typed once", "1234", "1234-add-search", "feature/1234-add-search"},
		{"adds ticket to prefixed name", "1234", "feature/add-search", "feature/1234-add-search"},
		{"preserves declared type", "1234", "fix/flaky login", "fix/1234-flaky-login"},
		{"ticket kept verbatim", "ABC-9", "add search", "feature/ABC-9-add-search"},
		{"ticket kept verbatim once", "ABC-9", "abc-9-add-search", "feature/ABC-9-add-search"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := everonBranchName(tc.ticket, tc.choice)
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if got != tc.want {
				t.Fatalf("expected %s, got %s", tc.want, got)
			}
		})
	}
}

func TestEveronBranchNameRejectsEmptyInputs(t *testing.T) {
	if _, err := everonBranchName("", "add-search"); err == nil {
		t.Fatal("expected error for empty ticket")
	}
	if _, err := everonBranchName("1234", "///"); err == nil {
		t.Fatal("expected error for empty branch name")
	}
	if _, err := everonBranchName("1234", "1234"); err == nil {
		t.Fatal("expected error when only the ticket is supplied")
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Add Search":      "add-search",
		"  spaced  out  ": "spaced-out",
		"CamelCase":       "camelcase",
		"a/b":             "a-b",
		"!!!":             "",
		"v2.1 release":    "v2-1-release",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Fatalf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBranchNameFromChoiceRejectsEmptyBaseBranchWhenChoiceEmpty(t *testing.T) {
	if _, _, err := branchNameFromChoice("1234", "", ""); err == nil {
		t.Fatal("expected error for empty source branch")
	}
}

func TestSwitchToExistingBranchTracksRemoteWhenLocalMissing(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	if out, err := exec.Command("git", "init", "--bare", origin).CombinedOutput(); err != nil {
		t.Fatalf("git init --bare failed: %v\n%s", err, string(out))
	}

	seed := filepath.Join(root, "seed")
	if out, err := exec.Command("git", "clone", origin, seed).CombinedOutput(); err != nil {
		t.Fatalf("git clone seed failed: %v\n%s", err, string(out))
	}
	runSeed := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = seed
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
		}
	}
	runSeed("config", "user.name", "Test User")
	runSeed("config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	runSeed("add", "README.md")
	runSeed("commit", "-m", "init")
	runSeed("push", "-u", "origin", "HEAD")
	runSeed("switch", "-c", "feature/remote")
	if err := os.WriteFile(filepath.Join(seed, "feature.txt"), []byte("feature"), 0o644); err != nil {
		t.Fatal(err)
	}
	runSeed("add", "feature.txt")
	runSeed("commit", "-m", "feature")
	runSeed("push", "-u", "origin", "feature/remote")

	clone := filepath.Join(root, "clone")
	if out, err := exec.Command("git", "clone", origin, clone).CombinedOutput(); err != nil {
		t.Fatalf("git clone test repo failed: %v\n%s", err, string(out))
	}

	if err := switchToExistingBranch(clone, "feature/remote"); err != nil {
		t.Fatalf("expected remote tracking switch to succeed, got %v", err)
	}

	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = clone
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse failed: %v\n%s", err, string(out))
	}
	if got := strings.TrimSpace(string(out)); got != "feature/remote" {
		t.Fatalf("expected current branch feature/remote, got %s", got)
	}
}

func TestCopyTreeCopiesDotfiles(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	if err := os.WriteFile(filepath.Join(src, ".env"), []byte("A=1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(src, ".github"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, ".github", "config.yml"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := copyTree(src, dst); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dst, ".env")); err != nil {
		t.Fatalf("expected .env to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, ".github", "config.yml")); err != nil {
		t.Fatalf("expected nested dotfile to exist: %v", err)
	}
}

func TestTasksPersistAndDelete(t *testing.T) {
	root := t.TempDir()
	app := &App{TasksToml: filepath.Join(root, "tasks.toml")}

	if err := app.addTask(Task{Branch: "feat/a", Path: "/tmp/repoA", Repo: "repoA"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := app.loadTasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tasks) != 1 || cfg.Tasks[0].Branch != "feat/a" {
		t.Fatalf("unexpected tasks: %+v", cfg.Tasks)
	}

	cfg.Tasks = cfg.Tasks[:0]
	if err := writeToml(app.TasksToml, cfg); err != nil {
		t.Fatal(err)
	}

	cfg, err = app.loadTasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tasks) != 0 {
		t.Fatalf("expected no tasks, got %+v", cfg.Tasks)
	}
}

func TestBuildRepoURL(t *testing.T) {
	got := buildRepoURL("https://gitlab.com/everon-technologies/", "/ec-backend/")
	if got != "https://gitlab.com/everon-technologies/ec-backend" {
		t.Fatalf("unexpected URL: %s", got)
	}
}

func TestBuildCloneURLUsesSSH(t *testing.T) {
	got := buildCloneURL("https://gitlab.com/everon-technologies/", "ec-backend")
	if got != "git@gitlab.com:everon-technologies/ec-backend" {
		t.Fatalf("unexpected clone URL: %s", got)
	}
}

func TestListAssetProjectsMissingFolderIsNotError(t *testing.T) {
	root := t.TempDir()
	projects, err := listAssetProjects(filepath.Join(root, "missing-assets"))
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(projects) != 0 {
		t.Fatalf("expected no projects, got %+v", projects)
	}
}

func TestFuzzyMatchProjects(t *testing.T) {
	projects := []string{"ec-backend", "ec-frontend", "platform-api"}
	matches := fuzzyMatchProjects("ecb", projects)
	if len(matches) != 1 || matches[0] != "ec-backend" {
		t.Fatalf("unexpected matches: %+v", matches)
	}
}

func TestResolveRepoQueryReturnsSingleFuzzyMatch(t *testing.T) {
	repo, err := resolveRepoQuery("ecb", []string{"ec-backend", "ec-frontend", "platform-api"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if repo != "ec-backend" {
		t.Fatalf("expected ec-backend, got %s", repo)
	}
}

func TestSelectRepoFromAssetsQueryUsesFuzzyMatch(t *testing.T) {
	root := t.TempDir()
	assets := filepath.Join(root, "assets")
	if err := os.MkdirAll(filepath.Join(assets, "ec-backend"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(assets, "platform-api"), 0o755); err != nil {
		t.Fatal(err)
	}

	app := &App{AssetsDir: assets}
	repo, err := app.selectRepoFromAssetsQuery("ecb")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if repo != "ec-backend" {
		t.Fatalf("expected ec-backend, got %s", repo)
	}
}

func TestResolveReposResolvesMultipleArgsAndDedupes(t *testing.T) {
	root := t.TempDir()
	assets := filepath.Join(root, "assets")
	for _, name := range []string{"ec-backend", "ec-frontend", "platform-api"} {
		if err := os.MkdirAll(filepath.Join(assets, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	app := &App{AssetsDir: assets}
	repos, err := app.resolveRepos([]string{"ecb", "ecf", "ecb"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	want := []string{"ec-backend", "ec-frontend"}
	if len(repos) != len(want) {
		t.Fatalf("expected %v, got %v", want, repos)
	}
	for i, r := range want {
		if repos[i] != r {
			t.Fatalf("expected %v, got %v", want, repos)
		}
	}
}

func TestResolveReposReportsUnresolvableArg(t *testing.T) {
	root := t.TempDir()
	assets := filepath.Join(root, "assets")
	if err := os.MkdirAll(filepath.Join(assets, "ec-backend"), 0o755); err != nil {
		t.Fatal(err)
	}

	app := &App{AssetsDir: assets}
	// "nope" matches nothing; resolveRepoQuery falls back to an interactive
	// confirm that fails on a non-TTY, so resolveRepos should surface an error
	// naming the offending query rather than silently succeeding.
	if _, err := app.resolveRepos([]string{"nope"}); err == nil {
		t.Fatal("expected error for unresolvable query")
	} else if !strings.Contains(err.Error(), "nope") {
		t.Fatalf("expected error to name the query, got %v", err)
	}
}

func TestTaskDisplayNameShortensHomePath(t *testing.T) {
	origHome := userHomeDir
	t.Cleanup(func() { userHomeDir = origHome })
	userHomeDir = func() (string, error) { return "/home/alice", nil }

	got := taskDisplayName(Task{Branch: "feat/a", Path: "/home/alice/work/pulley"})
	if got != "feat/a -> ~/work/pulley" {
		t.Fatalf("unexpected display name: %s", got)
	}
}

func TestTaskMenuItemUsesDescriptionOutsideFilter(t *testing.T) {
	item := taskMenuItem(Task{
		Branch: "feat/a",
		Repo:   "ec-backend",
		Path:   "/very/long/path/to/workspaces/backend/service",
	}, 3)

	if item.FilterValue() != "feat/a -> /very/long/path/to/workspaces/backend/service" {
		t.Fatalf("unexpected filter value: %s", item.FilterValue())
	}
	if item.Description() != "repo: ec-backend | path: /very/long/path/to/workspaces/backend/service" {
		t.Fatalf("unexpected description: %s", item.Description())
	}
	if got := item.selectionValue(); got != "3" {
		t.Fatalf("expected selection value 3, got %s", got)
	}
}

func TestTaskMenuItemUsesCustomDescriptionWhenPresent(t *testing.T) {
	item := taskMenuItem(Task{
		Branch:      "feat/a",
		Repo:        "ec-backend",
		Path:        "/very/long/path/to/workspaces/backend/service",
		Description: "sync billing webhooks",
	}, 0)
	if item.Description() != "sync billing webhooks" {
		t.Fatalf("unexpected custom description: %s", item.Description())
	}
	if item.FilterValue() != "feat/a -> /very/long/path/to/workspaces/backend/service" {
		t.Fatalf("unexpected filter value: %s", item.FilterValue())
	}
}

func TestCurrentGitTaskErrorsOutsideGitRepo(t *testing.T) {
	tmp := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}

	if _, err := currentGitTask(); err == nil {
		t.Fatal("expected error outside git repository")
	}
}

func TestAddCurrentTaskPersistsCurrentGitBranchAndPath(t *testing.T) {
	origPromptTaskDesc := promptTaskDesc
	promptTaskDesc = func(existing string) (string, error) { return "manual task desc", nil }
	t.Cleanup(func() { promptTaskDesc = origPromptTaskDesc })

	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
		}
	}

	run("init")
	run("config", "user.name", "Test User")
	run("config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-m", "init")
	run("switch", "-c", "feature/add-task")

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	app := &App{TasksToml: filepath.Join(t.TempDir(), "tasks.toml")}
	out := captureStdout(t, func() {
		if err := app.addCurrentTask(); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})
	if !strings.Contains(out, "Added task:") {
		t.Fatalf("expected add confirmation output, got %q", out)
	}
	if !strings.Contains(out, "feature/add-task") {
		t.Fatalf("expected output to include branch name, got %q", out)
	}
	if !strings.Contains(out, repo) {
		t.Fatalf("expected output to include repo path, got %q", out)
	}

	cfg, err := app.loadTasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tasks) != 1 {
		t.Fatalf("expected one task, got %d", len(cfg.Tasks))
	}
	if cfg.Tasks[0].Branch != "feature/add-task" {
		t.Fatalf("expected branch feature/add-task, got %s", cfg.Tasks[0].Branch)
	}
	if cfg.Tasks[0].Path != repo {
		t.Fatalf("expected path %s, got %s", repo, cfg.Tasks[0].Path)
	}
	if cfg.Tasks[0].Repo != filepath.Base(repo) {
		t.Fatalf("expected repo %s, got %s", filepath.Base(repo), cfg.Tasks[0].Repo)
	}
	if cfg.Tasks[0].Description != "manual task desc" {
		t.Fatalf("expected saved description, got %q", cfg.Tasks[0].Description)
	}
}

func TestAddCurrentTaskUsesCurrentSubfolderPath(t *testing.T) {
	origPromptTaskDesc := promptTaskDesc
	promptTaskDesc = func(existing string) (string, error) { return "", nil }
	t.Cleanup(func() { promptTaskDesc = origPromptTaskDesc })

	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
		}
	}

	run("init")
	run("config", "user.name", "Test User")
	run("config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README.md")
	run("commit", "-m", "init")
	run("switch", "-c", "feature/add-task")

	subdir := filepath.Join(repo, "services", "api")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	if err := os.Chdir(subdir); err != nil {
		t.Fatal(err)
	}

	app := &App{TasksToml: filepath.Join(t.TempDir(), "tasks.toml")}
	out := captureStdout(t, func() {
		if err := app.addCurrentTask(); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})
	if !strings.Contains(out, "Added task:") {
		t.Fatalf("expected add confirmation output, got %q", out)
	}
	if !strings.Contains(out, "feature/add-task") {
		t.Fatalf("expected output to include branch name, got %q", out)
	}
	if !strings.Contains(out, subdir) {
		t.Fatalf("expected output to include subdir path, got %q", out)
	}

	cfg, err := app.loadTasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tasks) != 1 {
		t.Fatalf("expected one task, got %d", len(cfg.Tasks))
	}
	if cfg.Tasks[0].Path != subdir {
		t.Fatalf("expected task path to be current folder %s, got %s", subdir, cfg.Tasks[0].Path)
	}
}

func TestWriteTomlOmitsEmptyTaskDescription(t *testing.T) {
	root := t.TempDir()
	tasksPath := filepath.Join(root, "tasks.toml")
	cfg := taskConfig{
		Tasks: []Task{
			{Branch: "feat/a", Path: "/tmp/repo", Repo: "repo", Description: "has desc"},
		},
	}
	if err := writeToml(tasksPath, cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Tasks[0].Description = ""
	if err := writeToml(tasksPath, cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(tasksPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "description =") {
		t.Fatalf("expected empty description to be removed from tasks.toml, got:\n%s", string(raw))
	}
}

func TestRenameCurrentTaskRenamesDirectoryAndUpdatesTaskPath(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	tasksToml := filepath.Join(root, "tasks.toml")
	app := &App{TasksToml: tasksToml}
	if err := app.addTask(Task{Branch: "feat/a", Path: repo, Repo: "repo"}); err != nil {
		t.Fatal(err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	if err := app.renameCurrentTask("repo-renamed"); err != nil {
		t.Fatalf("expected rename to succeed, got %v", err)
	}

	renamed := filepath.Join(root, "repo-renamed")
	if _, err := os.Stat(renamed); err != nil {
		t.Fatalf("expected renamed folder to exist: %v", err)
	}
	if _, err := os.Stat(repo); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected old folder to be gone, got err=%v", err)
	}

	cfg, err := app.loadTasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tasks) != 1 {
		t.Fatalf("expected one task, got %d", len(cfg.Tasks))
	}
	if got := cfg.Tasks[0].Path; got != renamed {
		t.Fatalf("expected updated task path %q, got %q", renamed, got)
	}
}

func TestRenameCurrentTaskErrorsWhenCurrentDirNotListed(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	app := &App{TasksToml: filepath.Join(root, "tasks.toml")}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}

	err = app.renameCurrentTask("repo-renamed")
	if err == nil {
		t.Fatal("expected error when current dir is not listed in tasks")
	}
	if !strings.Contains(err.Error(), "not listed") {
		t.Fatalf("expected not listed error, got %v", err)
	}
}

func TestInferTicketFromBranch(t *testing.T) {
	cases := []struct {
		branch string
		want   string
	}{
		{"123-add-search", "123"},
		{"PROJ-fix-bug", "PROJ"},
		{"feature/no-ticket", ""},
		{"main", ""},
		{"", ""},
		{"123", ""},
		{"-leading-dash", ""},
		{"has space-name", ""},
	}
	for _, c := range cases {
		got := inferTicketFromBranch(c.branch)
		if got != c.want {
			t.Errorf("inferTicketFromBranch(%q) = %q, want %q", c.branch, got, c.want)
		}
	}
}

func TestTaskMenuItemWithTicketPrefixesTitle(t *testing.T) {
	item := taskMenuItem(Task{
		Ticket: "123",
		Branch: "123-add-search",
		Repo:   "backend",
		Path:   "/work/backend",
	}, 1)

	if item.Title() != "[#123] 123-add-search -> /work/backend" {
		t.Fatalf("unexpected title: %s", item.Title())
	}
	if item.FilterValue() != "[#123] 123-add-search -> /work/backend" {
		t.Fatalf("unexpected filter value: %s", item.FilterValue())
	}
}

func TestTaskMenuItemWithoutTicketHasNoPrefix(t *testing.T) {
	item := taskMenuItem(Task{
		Branch: "feat/a",
		Repo:   "backend",
		Path:   "/work/backend",
	}, 0)

	if item.Title() != "feat/a -> /work/backend" {
		t.Fatalf("unexpected title (no prefix expected): %s", item.Title())
	}
}

func TestTasksTomlRoundtripsTicket(t *testing.T) {
	root := t.TempDir()
	app := &App{TasksToml: filepath.Join(root, "tasks.toml")}

	if err := app.addTask(Task{
		Ticket: "42",
		Branch: "42-fix",
		Path:   "/tmp/repo",
		Repo:   "backend",
	}); err != nil {
		t.Fatal(err)
	}

	cfg, err := app.loadTasks()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tasks) != 1 {
		t.Fatalf("expected one task, got %d", len(cfg.Tasks))
	}
	if cfg.Tasks[0].Ticket != "42" {
		t.Fatalf("expected ticket 42, got %q", cfg.Tasks[0].Ticket)
	}
}

func TestWriteTomlOmitsEmptyTicket(t *testing.T) {
	root := t.TempDir()
	tasksPath := filepath.Join(root, "tasks.toml")
	cfg := taskConfig{
		Tasks: []Task{
			{Ticket: "", Branch: "feat/a", Path: "/tmp/repo", Repo: "repo"},
		},
	}
	if err := writeToml(tasksPath, cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(tasksPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "ticket =") {
		t.Fatalf("expected empty ticket to be omitted from tasks.toml, got:\n%s", string(raw))
	}
}

func TestTmuxOpenWindowRequiresPaths(t *testing.T) {
	if err := tmuxOpenWindow("123", nil); err == nil {
		t.Fatal("expected error for empty paths")
	}
}

func TestTmuxOpenGroupRequiresPaths(t *testing.T) {
	if err := tmuxOpenGroup("123", nil); err == nil {
		t.Fatal("expected error for empty paths")
	}
}

func TestTmuxOpenGroupReportsPathsWhenTmuxMissing(t *testing.T) {
	origLookPath := lookPath
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(func() { lookPath = origLookPath })

	err := tmuxOpenGroup("123", []string{"/work/a", "/work/b"})
	if err == nil {
		t.Fatal("expected error when tmux is unavailable")
	}
	if !strings.Contains(err.Error(), "/work/a") || !strings.Contains(err.Error(), "/work/b") {
		t.Fatalf("expected the pulled paths in the error, got %v", err)
	}
}

func TestTmuxSessionNameSanitizesTicket(t *testing.T) {
	if got := tmuxSessionName("12.34"); got != "ticket-12-34" {
		t.Fatalf("expected dots replaced, got %s", got)
	}
	if got := tmuxSessionName(""); got != "ticket-pulley" {
		t.Fatalf("expected fallback name, got %s", got)
	}
}

func TestResolveShellPrefersFish(t *testing.T) {
	origLookPath := lookPath
	lookPath = func(name string) (string, error) {
		if name == "fish" {
			return "/opt/homebrew/bin/fish", nil
		}
		return "", exec.ErrNotFound
	}
	t.Cleanup(func() { lookPath = origLookPath })
	t.Setenv("PULLEY_SHELL", "")
	t.Setenv("SHELL", "/bin/zsh")

	if got := resolveShell(); got != "/opt/homebrew/bin/fish" {
		t.Fatalf("expected fish to win over $SHELL, got %s", got)
	}
}

func TestResolveShellFallsBackToShellEnv(t *testing.T) {
	origLookPath := lookPath
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(func() { lookPath = origLookPath })
	t.Setenv("PULLEY_SHELL", "")
	t.Setenv("SHELL", "/bin/zsh")

	if got := resolveShell(); got != "/bin/zsh" {
		t.Fatalf("expected $SHELL fallback, got %s", got)
	}
}

func TestResolveShellHonorsOverride(t *testing.T) {
	origLookPath := lookPath
	lookPath = func(string) (string, error) { return "/opt/homebrew/bin/fish", nil }
	t.Cleanup(func() { lookPath = origLookPath })
	t.Setenv("PULLEY_SHELL", "/bin/bash")

	if got := resolveShell(); got != "/bin/bash" {
		t.Fatalf("expected PULLEY_SHELL override, got %s", got)
	}
}

func TestPromptBatchInputAsksBranchAndDescriptionOnce(t *testing.T) {
	origAsk, origDesc := askInput, promptTaskDesc
	inputCalls, descCalls := 0, 0
	askInput = func(string, string) (string, error) {
		inputCalls++
		return "Add Search Bar", nil
	}
	promptTaskDesc = func(string) (string, error) {
		descCalls++
		return "wire up search", nil
	}
	t.Cleanup(func() { askInput, promptTaskDesc = origAsk, origDesc })

	var got batchInput
	out := captureStdout(t, func() {
		var err error
		got, err = promptBatchInput("1234")
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	if inputCalls != 1 || descCalls != 1 {
		t.Fatalf("expected one prompt each, got branch=%d description=%d", inputCalls, descCalls)
	}
	if !got.shared {
		t.Fatal("expected batch input to be marked shared")
	}
	if got.ticket != "1234" || got.branchLabel != "Add Search Bar" || got.description != "wire up search" {
		t.Fatalf("unexpected batch input: %+v", got)
	}
	if !strings.Contains(out, "feature/1234-add-search-bar") {
		t.Fatalf("expected converted branch preview, got %q", out)
	}
}

// A batch answers the branch question once, so every repo must land on the same
// branch name regardless of the base branch it was cut from.
func TestBatchInputProducesSameBranchAcrossRepos(t *testing.T) {
	input := batchInput{ticket: "1234", branchLabel: "Add Search Bar", shared: true}
	for _, baseBranch := range []string{"main", "dev", "release/2.1"} {
		branch, createNew, err := branchNameFromChoice(input.ticket, baseBranch, input.branchLabel)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if !createNew {
			t.Fatalf("expected a new branch off %s", baseBranch)
		}
		if branch != "feature/1234-add-search-bar" {
			t.Fatalf("expected identical branch off %s, got %s", baseBranch, branch)
		}
	}
}

// An empty batch branch label keeps the old "stay on the base branch" escape
// hatch, which is resolved per repo.
func TestBatchInputWithoutLabelUsesBaseBranch(t *testing.T) {
	branch, createNew, err := branchNameFromChoice("1234", "dev", "")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if createNew {
		t.Fatal("expected no new branch to be created")
	}
	if branch != "dev" {
		t.Fatalf("expected base branch dev, got %s", branch)
	}
}

func TestResolveDestinationNameAutoNamesBatch(t *testing.T) {
	root := t.TempDir()
	app := &App{PullRoot: root}

	var got string
	captureStdout(t, func() {
		var err error
		got, err = app.resolveDestinationName(batchInput{ticket: "1234", shared: true}, "ec-backend")
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})
	if got != "1234-ec-backend" {
		t.Fatalf("expected auto folder name, got %s", got)
	}
}

func TestResolveDestinationNameBatchAvoidsCollisions(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "1234-ec-backend"), 0o755); err != nil {
		t.Fatal(err)
	}
	app := &App{PullRoot: root}

	var got string
	captureStdout(t, func() {
		var err error
		got, err = app.resolveDestinationName(batchInput{ticket: "1234", shared: true}, "ec-backend")
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})
	if got != "1234-ec-backend2" {
		t.Fatalf("expected suffixed folder name, got %s", got)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe setup failed: %v", err)
	}
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = origStdout })

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("pipe close failed: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("stdout read failed: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("pipe reader close failed: %v", err)
	}
	return string(data)
}
