package pulley

import (
	"os"
	"path/filepath"
	"testing"
)

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
