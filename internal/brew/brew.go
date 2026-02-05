package brew

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

var ErrBrewNotFound = errors.New("brew not found")

func FindBrew() (string, error) {
	path, err := exec.LookPath("brew")
	if err != nil {
		return "", ErrBrewNotFound
	}
	return path, nil
}

func ListInstalled() (map[string]string, map[string]string, error) {
	formulae, err := listVersions([]string{"list", "--versions"})
	if err != nil {
		return nil, nil, err
	}
	casks, err := listVersions([]string{"list", "--cask", "--versions"})
	if err != nil {
		return nil, nil, err
	}
	return formulae, casks, nil
}

func Update(verbose bool) error {
	args := []string{"update"}
	out, err := run(args, verbose)
	if verbose && out != "" {
		fmt.Print(out)
	}
	return err
}

func UpgradeFormula(names []string, verbose bool) error {
	if len(names) == 0 {
		return nil
	}
	args := append([]string{"upgrade"}, names...)
	out, err := run(args, verbose)
	if verbose && out != "" {
		fmt.Print(out)
	}
	return err
}

func UpgradeCask(names []string, includeAutoUpdate bool, verbose bool) error {
	if len(names) == 0 {
		return nil
	}
	args := []string{"upgrade", "--cask"}
	if includeAutoUpdate {
		args = append(args, "--greedy")
	}
	args = append(args, names...)
	out, err := run(args, verbose)
	if verbose && out != "" {
		fmt.Print(out)
	}
	return err
}

func OutdatedFormula(names []string) ([]string, error) {
	if len(names) == 0 {
		return []string{}, nil
	}
	args := append([]string{"outdated", "--quiet", "--formula"}, names...)
	out, err := run(args, false)
	if err != nil {
		return nil, err
	}
	return parseOutdated(out), nil
}

func OutdatedCask(names []string, includeAutoUpdate bool) ([]string, error) {
	if len(names) == 0 {
		return []string{}, nil
	}
	args := []string{"outdated", "--quiet", "--cask"}
	if includeAutoUpdate {
		args = append(args, "--greedy")
	}
	args = append(args, names...)
	out, err := run(args, false)
	if err != nil {
		return nil, err
	}
	return parseOutdated(out), nil
}

func HasRunningBrew() (bool, error) {
	cmd := exec.Command("pgrep", "-x", "brew")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(string(out)) != "", nil
}

func listVersions(args []string) (map[string]string, error) {
	out, err := run(args, false)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		version := fields[1]
		result[name] = version
	}
	return result, nil
}

func run(args []string, verbose bool) (string, error) {
	brewPath, err := FindBrew()
	if err != nil {
		return "", err
	}
	cmd := exec.Command(brewPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String(), fmt.Errorf("brew %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	if verbose && stderr.Len() > 0 {
		return stdout.String() + "\n" + stderr.String(), nil
	}
	return stdout.String(), nil
}

func parseOutdated(out string) []string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		result = append(result, fields[0])
	}
	return result
}
