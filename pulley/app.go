package pulley

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/huh"
)

const (
	sourceFileName = "source.toml"
	tasksFileName  = "tasks.toml"
)

var (
	executablePath = os.Executable
	evalSymlinks   = filepath.EvalSymlinks
	userHomeDir    = os.UserHomeDir
	lookPath       = exec.LookPath
	promptTaskDesc = promptTaskDescription
	editTaskPrompt = promptTaskEditor
)

type App struct {
	AssetsDir  string
	PullRoot   string
	SourceToml string
	TasksToml  string
}

func promptOptionalInput(label, placeholder string) (string, error) {
	input, err := promptOptionalInputWithGum(label, placeholder)
	if err == nil {
		return input, nil
	}
	if !errors.Is(err, exec.ErrNotFound) {
		return "", err
	}
	return promptOptionalInputFallback(label, placeholder)
}

func promptOptionalInputWithGum(label, placeholder string) (string, error) {
	if _, err := lookPath("gum"); err != nil {
		return "", err
	}
	args := []string{"input", "--prompt", label + ": "}
	if placeholder != "" {
		args = append(args, "--placeholder", placeholder)
	}
	cmd := exec.Command("gum", args...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func promptOptionalInputFallback(label, placeholder string) (string, error) {
	fmt.Printf("%s", label)
	if placeholder != "" {
		fmt.Printf(" [%s]", placeholder)
	}
	fmt.Print(": ")
	reader := bufio.NewReader(os.Stdin)
	text, err := reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(text), nil
}

type sourceConfig struct {
	Source string `toml:"source"`
}

type taskConfig struct {
	Tasks []Task `toml:"tasks"`
}

type Task struct {
	Branch      string    `toml:"branch"`
	Path        string    `toml:"path"`
	Repo        string    `toml:"repo"`
	Description string    `toml:"description,omitempty"`
	CreatedAt   time.Time `toml:"created_at"`
}

func NewDefaultApp() (*App, error) {
	base, err := commandBaseDir()
	if err != nil {
		return nil, err
	}
	home, err := userHomeDir()
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

func commandBaseDir() (string, error) {
	exePath, err := executablePath()
	if err != nil {
		return "", err
	}
	if resolved, err := evalSymlinks(exePath); err == nil {
		exePath = resolved
	}
	return filepath.Dir(exePath), nil
}

func (a *App) Run(args []string) error {
	if len(args) > 0 && args[0] == "task" {
		if len(args) > 1 && args[1] == "add" {
			return a.addCurrentTask()
		}
		if len(args) > 1 && args[1] == "rename" {
			name := ""
			if len(args) > 2 {
				name = args[2]
			}
			return a.renameCurrentTask(name)
		}
		return a.runTaskSelector()
	}

	if err := os.MkdirAll(a.AssetsDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(a.PullRoot, 0o755); err != nil {
		return err
	}

	repo := ""
	if len(args) > 0 {
		selected, err := a.selectRepoFromAssetsQuery(args[0])
		if err != nil {
			return fmt.Errorf("failed to resolve repository query: %w", err)
		}
		repo = selected
	} else {
		selected, err := a.selectRepoFromAssetsOrNew()
		if err != nil {
			return err
		}
		repo = selected
	}
	fmt.Printf("Pulling repo: %s\n", repo)

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
		source, err = a.promptAndSaveSource()
		if err != nil {
			return err
		}
	}
	cloneURL := buildCloneURL(source, repo)
	ticket, err := promptTicketNumber()
	if err != nil {
		return err
	}
	destName, err := promptDestinationName(a.PullRoot, ticket, repo)
	if err != nil {
		return err
	}
	destPath := filepath.Join(a.PullRoot, destName)

	if err := runCmd("", "git", "clone", cloneURL, destPath); err != nil {
		return err
	}

	assetTemplate := filepath.Join(a.AssetsDir, repo)
	if st, err := os.Stat(assetTemplate); err == nil && st.IsDir() {
		if err := copyTree(assetTemplate, destPath); err != nil {
			return err
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(assetTemplate, 0o755); err != nil {
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
	newBranch, createNewBranch, err := promptBranchName(ticket, baseBranch)
	if err != nil {
		return err
	}
	fmt.Printf("%s -> %s\n", baseBranch, newBranch)
	if createNewBranch {
		if err := runCmd(destPath, "git", "switch", "-c", newBranch, baseBranch); err != nil {
			return err
		}
	} else {
		if err := switchToExistingBranch(destPath, newBranch); err != nil {
			return err
		}
	}

	description, err := promptTaskDesc("")
	if err != nil {
		return err
	}
	if err := a.addTask(Task{
		Branch:      newBranch,
		Path:        destPath,
		Repo:        repo,
		Description: description,
		CreatedAt:   time.Now().UTC(),
	}); err != nil {
		return err
	}

	fmt.Printf("Created %s and branch %s\n", destPath, newBranch)
	return launchShell(destPath)
}

func (a *App) loadSource() (string, error) {
	var cfg sourceConfig
	if _, err := os.Stat(a.SourceToml); err != nil {
		return "", errors.New("source not configured")
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

func buildCloneURL(source, repo string) string {
	source = strings.TrimSpace(source)
	repo = strings.Trim(strings.TrimSpace(repo), "/")
	if source == "" {
		return repo
	}

	if strings.HasPrefix(source, "git@") {
		src := strings.TrimRight(source, "/")
		if strings.Contains(src, ":") {
			return src + "/" + repo
		}
		return src + ":" + repo
	}

	if strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "http://") {
		u, err := url.Parse(source)
		if err == nil && u.Host != "" {
			path := strings.Trim(u.Path, "/")
			if path == "" {
				return fmt.Sprintf("git@%s:%s", u.Host, repo)
			}
			return fmt.Sprintf("git@%s:%s/%s", u.Host, path, repo)
		}
		if err != nil {
			return strings.TrimRight(source, "/") + "/" + repo
		}
	}

	src := strings.Trim(source, "/")
	parts := strings.SplitN(src, "/", 2)
	if len(parts) == 2 {
		return fmt.Sprintf("git@%s:%s/%s", parts[0], parts[1], repo)
	}
	return src + "/" + repo
}

func (a *App) promptAndSaveSource() (string, error) {
	source, err := promptInput("Set source git namespace", "")
	if err != nil {
		return "", err
	}
	source = strings.TrimSpace(source)
	if source == "" {
		return "", errors.New("source cannot be empty")
	}
	if err := a.saveSource(source); err != nil {
		return "", err
	}
	return source, nil
}

func promptTicketNumber() (string, error) {
	ticket, err := promptInput("Ticket number", "")
	if err != nil {
		return "", err
	}
	ticket = strings.TrimSpace(ticket)
	if ticket == "" {
		return "", errors.New("ticket number cannot be empty")
	}
	fmt.Printf("#%s\n", ticket)
	return ticket, nil
}

func ticketPrefixedName(ticket, name string) (string, error) {
	ticket = strings.TrimSpace(ticket)
	name = strings.TrimSpace(name)
	if ticket == "" {
		return "", errors.New("ticket number cannot be empty")
	}
	if name == "" {
		return "", errors.New("name cannot be empty")
	}
	return fmt.Sprintf("%s-%s", ticket, name), nil
}

func promptDestinationName(root, ticket, repo string) (string, error) {
	autoName, err := ticketPrefixedName(ticket, repo)
	if err != nil {
		return "", err
	}
	choice, err := promptInput("Custom folder name (leave empty for auto)", autoName)
	if err != nil {
		return "", err
	}
	prefixed, err := destinationNameFromChoice(ticket, autoName, choice)
	if err != nil {
		return "", err
	}
	finalName := nextAvailableName(root, prefixed)
	fmt.Printf("Folder preview: %s\n", finalName)
	return finalName, nil
}

func destinationNameFromChoice(ticket, autoName, choice string) (string, error) {
	choice = strings.TrimSpace(choice)
	if choice == "" {
		return autoName, nil
	}
	ticket = strings.TrimSpace(ticket)
	if ticket != "" && strings.HasPrefix(choice, ticket+"-") {
		return choice, nil
	}
	return ticketPrefixedName(ticket, choice)
}

func promptBranchName(ticket, baseBranch string) (string, bool, error) {
	branchBase, err := promptInput("Name your branch (leave empty to use base branch)", "")
	if err != nil {
		return "", false, err
	}
	branch, createNewBranch, err := branchNameFromChoice(ticket, baseBranch, branchBase)
	if err != nil {
		return "", false, err
	}
	fmt.Printf("Branch preview: %s\n", branch)
	return branch, createNewBranch, nil
}

func branchNameFromChoice(ticket, baseBranch, choice string) (string, bool, error) {
	choice = strings.TrimSpace(choice)
	if choice == "" {
		baseBranch = strings.TrimSpace(baseBranch)
		if baseBranch == "" {
			return "", false, errors.New("base branch cannot be empty")
		}
		return baseBranch, false, nil
	}
	branch, err := ticketPrefixedName(ticket, choice)
	if err != nil {
		return "", false, fmt.Errorf("invalid branch name: %w", err)
	}
	return branch, true, nil
}

func switchToExistingBranch(repoPath, branch string) error {
	if err := runCmd(repoPath, "git", "switch", branch); err == nil {
		return nil
	}
	fmt.Printf("Branch %s not found locally, tracking origin/%s\n", branch, branch)
	if trackErr := runCmd(repoPath, "git", "switch", "--track", "origin/"+branch); trackErr != nil {
		return fmt.Errorf("switch to %s failed: %w", branch, trackErr)
	}
	return nil
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

func (a *App) selectRepoFromAssetsOrNew() (string, error) {
	projects, err := listAssetProjects(a.AssetsDir)
	if err != nil {
		return "", err
	}

	query, err := promptInput("Find repo (fuzzy, blank to browse)", "")
	if err != nil {
		return "", err
	}
	query = strings.TrimSpace(query)

	if query == "" {
		if len(projects) == 0 {
			return "", errors.New("no project folders found in assets")
		}
		return pickFromList("Pick project", projects, false)
	}

	return resolveRepoQuery(query, projects)
}

func (a *App) selectRepoFromAssetsQuery(query string) (string, error) {
	projects, err := listAssetProjects(a.AssetsDir)
	if err != nil {
		return "", err
	}
	return resolveRepoQuery(query, projects)
}

func resolveRepoQuery(query string, projects []string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", errors.New("repo cannot be empty")
	}

	matches := fuzzyMatchProjects(query, projects)
	if len(matches) > 0 {
		if len(matches) == 1 {
			return matches[0], nil
		}
		return pickFromList("Pick project match", matches, false)
	}

	ok, err := confirm("No asset match. Try new pull? [y/N]: ")
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errors.New("selection cancelled")
	}
	return query, nil
}

func fuzzyMatchProjects(query string, projects []string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return append([]string(nil), projects...)
	}
	out := make([]string, 0, len(projects))
	for _, project := range projects {
		name := strings.ToLower(project)
		if strings.Contains(name, query) || isSubsequence(query, name) {
			out = append(out, project)
		}
	}
	sort.Strings(out)
	return out
}

func isSubsequence(query, target string) bool {
	if query == "" {
		return true
	}
	j := 0
	for i := 0; i < len(target) && j < len(query); i++ {
		if target[i] == query[j] {
			j++
		}
	}
	return j == len(query)
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
	if err != nil {
		return fmt.Errorf("copy file from %s to %s: %w", src, dst, err)
	}
	return nil
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
		if fallback, err := gitDefaultBranch(repoPath); err == nil && fallback != "" {
			branches = append(branches, fallback)
		}
	}
	if len(branches) == 0 {
		return nil, errors.New("no branches found to switch from")
	}
	return branches, nil
}

func gitDefaultBranch(repoPath string) (string, error) {
	cmd := exec.Command("git", "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	branch := strings.TrimSpace(string(out))
	branch = strings.TrimPrefix(branch, "origin/")
	return branch, nil
}

func runCmd(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func promptInput(label, defaultValue string) (string, error) {
	input, err := promptInputWithGum(label, defaultValue)
	if err == nil {
		return input, nil
	}
	if !errors.Is(err, exec.ErrNotFound) {
		return "", err
	}

	return promptInputFallback(label, defaultValue)
}

func promptInputWithGum(label, defaultValue string) (string, error) {
	if _, err := lookPath("gum"); err != nil {
		return "", err
	}
	args := []string{"input", "--prompt", label + ": "}
	if defaultValue != "" {
		args = append(args, "--placeholder", defaultValue)
	}
	cmd := exec.Command("gum", args...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return defaultValue, nil
	}
	return text, nil
}

func promptInputFallback(label, defaultValue string) (string, error) {
	fmt.Printf("%s", label)
	if defaultValue != "" {
		fmt.Printf(" [%s]", defaultValue)
	}
	fmt.Print(": ")
	reader := bufio.NewReader(os.Stdin)
	text, err := reader.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			return defaultValue, nil
		}
		return "", err
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
	options := make([]menuItem, 0, len(cfg.Tasks))
	for i, t := range cfg.Tasks {
		options = append(options, taskMenuItem(t, i))
	}
	picked, action, err := runDetailedMenu("Pick a task", options, true, true)
	if err != nil {
		return err
	}
	idx, err := strconv.Atoi(picked)
	if err != nil || idx < 0 || idx >= len(cfg.Tasks) {
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
	if action == "edit" {
		updated, err := editTaskPrompt(cfg.Tasks[idx])
		if err != nil {
			return err
		}
		cfg.Tasks[idx].Description = strings.TrimSpace(updated.Description)
		cfg.Tasks[idx].Branch = strings.TrimSpace(updated.Branch)
		cfg.Tasks[idx].Path = strings.TrimSpace(updated.Path)
		cfg.Tasks[idx].Repo = strings.TrimSpace(updated.Repo)
		if cfg.Tasks[idx].Branch == "" || cfg.Tasks[idx].Path == "" || cfg.Tasks[idx].Repo == "" {
			return errors.New("branch, path, and repo cannot be empty")
		}
		return writeToml(a.TasksToml, cfg)
	}

	return launchShell(cfg.Tasks[idx].Path)
}

func (a *App) addCurrentTask() error {
	baseTask, err := currentGitTask()
	if err != nil {
		return err
	}
	description, err := promptTaskDesc("")
	if err != nil {
		return err
	}
	t := Task{
		Branch:      baseTask.Branch,
		Path:        baseTask.Path,
		Repo:        baseTask.Repo,
		Description: description,
		CreatedAt:   baseTask.CreatedAt,
	}
	if err := a.addTask(t); err != nil {
		return err
	}
	fmt.Printf("Added task: %s\n", taskDisplayName(t))
	return nil
}

func (a *App) renameCurrentTask(nameArg string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, err := a.loadTasks()
	if err != nil {
		return err
	}
	cleanCwd := filepath.Clean(cwd)
	match := -1
	for i, t := range cfg.Tasks {
		if filepath.Clean(t.Path) == cleanCwd {
			match = i
			break
		}
	}
	if match < 0 {
		return errors.New("current directory is not listed in tasks.toml")
	}

	name := strings.TrimSpace(nameArg)
	if name == "" {
		name, err = promptOptionalInput("New folder name", filepath.Base(cleanCwd))
		if err != nil {
			return err
		}
		name = strings.TrimSpace(name)
	}
	if name == "" {
		return errors.New("new folder name cannot be empty")
	}
	newPath := filepath.Join(filepath.Dir(cleanCwd), name)
	if filepath.Clean(newPath) == cleanCwd {
		return errors.New("new folder name matches current directory")
	}
	if _, err := os.Stat(newPath); err == nil {
		return fmt.Errorf("destination already exists: %s", newPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(cleanCwd, newPath); err != nil {
		return err
	}
	cfg.Tasks[match].Path = newPath
	if err := writeToml(a.TasksToml, cfg); err != nil {
		return err
	}
	fmt.Printf("Renamed task path: %s -> %s\n", cleanCwd, newPath)
	return nil
}

func currentGitTask() (Task, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return Task{}, err
	}
	repoRoot, err := gitOutput(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return Task{}, fmt.Errorf("current directory is not in a git repository: %w", err)
	}
	branch, err := gitOutput(cwd, "branch", "--show-current")
	if err != nil {
		return Task{}, fmt.Errorf("failed to get current git branch: %w", err)
	}
	if strings.TrimSpace(branch) == "" {
		return Task{}, errors.New("current git branch could not be determined")
	}

	return Task{
		Branch:    strings.TrimSpace(branch),
		Path:      cwd,
		Repo:      filepath.Base(repoRoot),
		CreatedAt: time.Now().UTC(),
	}, nil
}

func taskDisplayName(t Task) string {
	return fmt.Sprintf("%s -> %s", t.Branch, shortenHomePath(t.Path))
}

func taskMenuItem(t Task, index int) menuItem {
	title := taskDisplayName(t)
	description := strings.TrimSpace(t.Description)
	if description == "" {
		description = fmt.Sprintf("repo: %s | path: %s", t.Repo, t.Path)
	}
	return menuItem{
		title:       title,
		description: description,
		filterValue: title,
		value:       strconv.Itoa(index),
	}
}

func promptTaskDescription(existing string) (string, error) {
	description, err := promptOptionalInput("Task description (optional)", existing)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(description), nil
}

func promptTaskEditor(current Task) (Task, error) {
	updated := Task{
		Description: strings.TrimSpace(current.Description),
		Branch:      strings.TrimSpace(current.Branch),
		Path:        strings.TrimSpace(current.Path),
		Repo:        strings.TrimSpace(current.Repo),
	}
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewText().
				Title("Description (optional)").
				Placeholder("Describe this task").
				Value(&updated.Description),
			huh.NewInput().
				Title("Branch").
				Placeholder("feature/my-task").
				Value(&updated.Branch),
			huh.NewInput().
				Title("Path").
				Placeholder("/abs/path/to/repo").
				Value(&updated.Path),
			huh.NewInput().
				Title("Repo").
				Placeholder("repo-name").
				Value(&updated.Repo),
		),
	)
	if err := form.Run(); err != nil {
		return Task{}, err
	}
	updated.Description = strings.TrimSpace(updated.Description)
	updated.Branch = strings.TrimSpace(updated.Branch)
	updated.Path = strings.TrimSpace(updated.Path)
	updated.Repo = strings.TrimSpace(updated.Repo)
	return updated, nil
}

func shortenHomePath(path string) string {
	home, err := userHomeDir()
	if err != nil || home == "" {
		return path
	}
	cleanPath := filepath.Clean(path)
	cleanHome := filepath.Clean(home)
	if cleanPath == cleanHome {
		return "~"
	}
	prefix := cleanHome + string(os.PathSeparator)
	if strings.HasPrefix(cleanPath, prefix) {
		return "~" + string(os.PathSeparator) + strings.TrimPrefix(cleanPath, prefix)
	}
	return path
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return "", fmt.Errorf("git %s failed: %s: %w", strings.Join(args, " "), msg, err)
		}
		return "", fmt.Errorf("git %s failed: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
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
		shell = "/bin/sh"
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
			return []string{}, nil
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
	return projects, nil
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
