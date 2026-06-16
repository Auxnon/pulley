package pulley

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	sourceFileName = "source.toml"
	tasksFileName  = "tasks.toml"
)

type App struct {
	AssetsDir  string
	PullRoot   string
	SourceToml string
	TasksToml  string
}

type sourceConfig struct {
	Source string `toml:"source"`
}

type taskConfig struct {
	Tasks []Task `toml:"tasks"`
}

type Task struct {
	Branch    string    `toml:"branch"`
	Path      string    `toml:"path"`
	Repo      string    `toml:"repo"`
	CreatedAt time.Time `toml:"created_at"`
}

func NewDefaultApp() (*App, error) {
	base, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return &App{
		AssetsDir:  filepath.Join(base, "assets"),
		PullRoot:   filepath.Join(home, "pulled"),
		SourceToml: filepath.Join(base, sourceFileName),
		TasksToml:  filepath.Join(base, tasksFileName),
	}, nil
}

func (a *App) Run(args []string) error {
	if len(args) > 0 && args[0] == "source" {
		if len(args) < 2 {
			return errors.New("usage: pulley source <git base url>")
		}
		return a.saveSource(args[1])
	}

	if len(args) > 0 && args[0] == "task" {
		return a.runTaskSelector()
	}

	if err := os.MkdirAll(a.PullRoot, 0o755); err != nil {
		return err
	}

	repo := ""
	if len(args) > 0 {
		repo = args[0]
	} else {
		selected, err := pickFromAssets(a.AssetsDir)
		if err != nil {
			return err
		}
		repo = selected
	}

	return a.pullRepo(repo)
}

func (a *App) saveSource(source string) error {
	cfg := sourceConfig{Source: strings.TrimSpace(source)}
	if cfg.Source == "" {
		return errors.New("source cannot be empty")
	}
	if err := writeToml(a.SourceToml, cfg); err != nil {
		return err
	}
	fmt.Printf("source set to %s\n", cfg.Source)
	return nil
}

func (a *App) pullRepo(repo string) error {
	source, err := a.loadSource()
	if err != nil {
		return err
	}
	url := buildRepoURL(source, repo)
	destName, err := promptDestinationName(a.PullRoot, repo)
	if err != nil {
		return err
	}
	destPath := filepath.Join(a.PullRoot, destName)

	if err := runCmd("", "git", "clone", url, destPath); err != nil {
		return err
	}

	assetTemplate := filepath.Join(a.AssetsDir, repo)
	if st, err := os.Stat(assetTemplate); err == nil && st.IsDir() {
		if err := copyTree(assetTemplate, destPath); err != nil {
			return err
		}
	}

	branches, err := gitBranches(destPath)
	if err != nil {
		return err
	}
	baseBranch, err := pickFromList("Select base branch", branches, false)
	if err != nil {
		return err
	}
	newBranch, err := promptInput("Name your branch", "")
	if err != nil {
		return err
	}
	newBranch = strings.TrimSpace(newBranch)
	if newBranch == "" {
		return errors.New("branch name cannot be empty")
	}
	if err := runCmd(destPath, "git", "switch", "-c", newBranch, baseBranch); err != nil {
		return err
	}

	if err := a.addTask(Task{Branch: newBranch, Path: destPath, Repo: repo, CreatedAt: time.Now().UTC()}); err != nil {
		return err
	}

	fmt.Printf("Created %s and branch %s\n", destPath, newBranch)
	return launchShell(destPath)
}

func (a *App) loadSource() (string, error) {
	var cfg sourceConfig
	if _, err := os.Stat(a.SourceToml); err != nil {
		return "", errors.New("source not configured, run: pulley source <git base url>")
	}
	if _, err := toml.DecodeFile(a.SourceToml, &cfg); err != nil {
		return "", err
	}
	cfg.Source = strings.TrimSpace(cfg.Source)
	if cfg.Source == "" {
		return "", errors.New("source is empty in source.toml")
	}
	return cfg.Source, nil
}

func buildRepoURL(source, repo string) string {
	return strings.TrimRight(strings.TrimSpace(source), "/") + "/" + strings.Trim(strings.TrimSpace(repo), "/")
}

func promptDestinationName(root, repo string) (string, error) {
	choice, err := promptInput("Custom folder name (leave empty for auto)", "")
	if err != nil {
		return "", err
	}
	choice = strings.TrimSpace(choice)
	if choice == "" {
		return nextAvailableName(root, repo), nil
	}
	if _, err := os.Stat(filepath.Join(root, choice)); err == nil {
		return nextAvailableName(root, choice), nil
	}
	return choice, nil
}

func nextAvailableName(root, base string) string {
	name := base
	if _, err := os.Stat(filepath.Join(root, name)); err != nil {
		return name
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s%d", base, i)
		if _, err := os.Stat(filepath.Join(root, candidate)); err != nil {
			return candidate
		}
	}
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, st.Mode().Perm())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = out.ReadFrom(in)
	return err
}

func gitBranches(repoPath string) ([]string, error) {
	cmd := exec.Command("git", "for-each-ref", "--format=%(refname:short)", "refs/heads", "refs/remotes/origin")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list branches: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	unique := map[string]struct{}{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "origin/")
		if line == "" || strings.Contains(line, "HEAD") {
			continue
		}
		unique[line] = struct{}{}
	}
	branches := make([]string, 0, len(unique))
	for k := range unique {
		branches = append(branches, k)
	}
	sort.Strings(branches)
	if len(branches) == 0 {
		branches = append(branches, "main")
	}
	return branches, nil
}

func runCmd(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func promptInput(label, defaultValue string) (string, error) {
	fmt.Printf("%s", label)
	if defaultValue != "" {
		fmt.Printf(" [%s]", defaultValue)
	}
	fmt.Print(": ")
	reader := bufio.NewReader(os.Stdin)
	text, err := reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, os.ErrClosed) {
			return defaultValue, nil
		}
		if errors.Is(err, syscall.EINVAL) {
			return defaultValue, nil
		}
		if errors.Is(err, syscall.ENOTTY) {
			return defaultValue, nil
		}
		if errors.Is(err, syscall.EIO) {
			return defaultValue, nil
		}
		if errors.Is(err, os.ErrPermission) {
			return defaultValue, nil
		}
		if strings.TrimSpace(text) == "" {
			return defaultValue, nil
		}
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return defaultValue, nil
	}
	return text, nil
}

func (a *App) addTask(t Task) error {
	cfg, err := a.loadTasks()
	if err != nil {
		return err
	}
	cfg.Tasks = append(cfg.Tasks, t)
	return writeToml(a.TasksToml, cfg)
}

func (a *App) loadTasks() (taskConfig, error) {
	cfg := taskConfig{Tasks: []Task{}}
	if _, err := os.Stat(a.TasksToml); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	if _, err := toml.DecodeFile(a.TasksToml, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (a *App) runTaskSelector() error {
	cfg, err := a.loadTasks()
	if err != nil {
		return err
	}
	if len(cfg.Tasks) == 0 {
		return errors.New("no tasks yet")
	}
	options := make([]string, 0, len(cfg.Tasks))
	indexByName := make(map[string]int, len(cfg.Tasks))
	for i, t := range cfg.Tasks {
		name := fmt.Sprintf("%s -> %s", t.Branch, t.Path)
		options = append(options, name)
		indexByName[name] = i
	}
	picked, action, err := pickWithDelete("Pick a task", options)
	if err != nil {
		return err
	}
	idx, ok := indexByName[picked]
	if !ok {
		return errors.New("invalid selection")
	}

	if action == "delete" {
		ok, err := confirm("Delete task and pulled repo? [y/N]: ")
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if err := os.RemoveAll(cfg.Tasks[idx].Path); err != nil {
			return err
		}
		cfg.Tasks = append(cfg.Tasks[:idx], cfg.Tasks[idx+1:]...)
		return writeToml(a.TasksToml, cfg)
	}

	return launchShell(cfg.Tasks[idx].Path)
}

func confirm(prompt string) (bool, error) {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false, err
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes", nil
}

func launchShell(dir string) error {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	cmd := exec.Command(shell)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Printf("Entering shell in %s\n", dir)
	return cmd.Run()
}

func listAssetProjects(assetsDir string) ([]string, error) {
	entries, err := os.ReadDir(assetsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("assets folder does not exist")
		}
		return nil, err
	}
	projects := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			projects = append(projects, e.Name())
		}
	}
	sort.Strings(projects)
	if len(projects) == 0 {
		return nil, errors.New("no project folders found in assets")
	}
	return projects, nil
}

func pickFromAssets(assetsDir string) (string, error) {
	projects, err := listAssetProjects(assetsDir)
	if err != nil {
		return "", err
	}
	return pickFromList("Pick project", projects, false)
}

func writeToml(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(v)
}
