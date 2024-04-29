package main

import (
	"bufio"
	"fmt"
	"github.com/charmbracelet/log"
	"github.com/ttacon/chalk"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

const VersionUnknown = "Unknown"
const VersionNotFound = "NotFound"

func main() {
	if len(os.Args) < 4 {
		log.Errorf("Usage: go-dep-updater <root_directory_path> <dependency> <target-version>")
		return
	}

	rootDir := os.Args[1]
	dependency := os.Args[2]
	targetVersion := os.Args[3]

	confirmBeforeEach := false

	if len(os.Args) >= 5 {
		confirmBeforeEach = os.Args[4] == "confirm-each"
	}

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && info.Name() == "go.mod" {
			projectDir := filepath.Dir(path)
			projectName := filepath.Base(projectDir)

			currentVersion, upgrade := shouldUpgrade(path, dependency, targetVersion)
			if !upgrade {
				log.Debugf("Upgrade not needed for %s\n", projectDir)
				return nil
			}

			if confirmBeforeEach {
				if answer := readInput("Continue with %s?", projectDir); answer != "y" && answer != "yes" {
					log.Debugf("Skipping %s\n", projectDir)
					return nil
				}
			}

			log.Infof("Updating Project: %s from version %s to %s", projectName, currentVersion, targetVersion)

			printIndentedInfo(projectName, "Checking for uncommitted changes...")
			if hasUncommittedChanges(projectDir) {
				printIndentedWarning(projectName, "Warning: Project %s has uncommitted changes. Skipping update.", projectName)
				return nil
			}

			printIndentedInfo(projectName, "Checking that current git branch is master...")
			currentBranch, err := currentGitBranch(projectDir)
			if err != nil {
				printIndentedError(projectName, "Error determining current branch for project %s: %v", projectName, err)
				return nil
			}

			if currentBranch != "master" {
				printIndentedInfo(projectName, "Project is not on 'master' branch. Switching...")

				err := gitCheckoutMaster(projectDir)
				if err != nil {
					printIndentedError(projectName, "Error switching to 'master' branch for project %s: %v", projectName, err)
					return nil
				}
			}

			printIndentedInfo(projectName, "Pulling latest from origin...")
			if err := gitPull(projectDir); err != nil {
				printIndentedError(projectName, "Error pulling changes for project %s: %v", projectName, err)
				return nil
			}

			printIndentedInfo(projectName, "Running go get...")
			if err := goGetUpdate(projectDir, dependency, targetVersion); err != nil {
				printIndentedError(projectName, "Error updating dependency for project %s: %v", projectName, err)
				return nil
			}

			printIndentedInfo(projectName, "Successfully updated dependency %s to %s for %s", dependency, targetVersion, projectName)

			printIndentedInfo(projectName, "Running go vet...")
			if err := goVet(projectDir); err != nil {
				printIndentedError(projectName, "Error running go vet for project %s: %v", projectName, err)
				return fmt.Errorf("aborted due to unwanted project state after update. See above error(s)")
			}

			printIndentedInfo(projectName, "Running go test...")
			if err := goTest(projectDir); err != nil {
				printIndentedError(projectName, "Error running go test for project %s: %v", projectName, err)
				return fmt.Errorf("aborted due to unwanted project state after update. See above error(s)")
			}

			if directoryHasFile(projectDir, "main.go") {
				printIndentedInfo(projectName, "Running go build...")

				if err := goBuild(projectDir); err != nil {
					printIndentedError(projectName, "Error running go build for project %s: %v", projectName, err)
					return fmt.Errorf("aborted due to unwanted project state after update")
				}
			}

			printIndentedInfo(projectName, "Committing changes to git...")
			if err := gitCommit(projectDir, dependency, targetVersion); err != nil {
				printIndentedError(projectName, "Error committing changes for project %s: %v", projectName, err)
				return nil
			}

			printIndentedInfo(projectName, "Pushing to git origin...")
			if err := gitPush(projectDir); err != nil {
				printIndentedError(projectName, "Error pushing changes for project %s: %v", projectName, err)
				return nil
			}

			printIndentedInfo("Done updating %s", projectName)
		}

		return nil
	})

	if err != nil {
		log.Errorf("Error walking the path: %v\n", err)
		return
	}
}

func shouldUpgrade(path, dependency, targetVersion string) (version string, upgrade bool) {
	currentVersion := getDependencyVersion(path, dependency)
	isKnownVersion := currentVersion != VersionUnknown && currentVersion != VersionNotFound
	return currentVersion, isKnownVersion && currentVersion != targetVersion
}

func hasUncommittedChanges(projectDir string) bool {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = projectDir
	out, _ := executeCommand(cmd)
	return len(out) > 0
}

func gitPull(projectDir string) error {
	cmd := exec.Command("git", "pull")
	cmd.Dir = projectDir
	out, err := executeCommand(cmd)
	if err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}
	return nil
}

func goGetUpdate(projectDir, dependency, targetVersion string) error {
	cmd := exec.Command("go", "get", fmt.Sprintf("%s@%s", dependency, targetVersion))
	cmd.Dir = projectDir
	out, err := executeCommand(cmd)
	if err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}

	cmd = exec.Command("go", "mod", "tidy")
	cmd.Dir = projectDir
	out, err = executeCommand(cmd)
	if err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}
	return nil
}

func goVet(projectDir string) error {
	cmd := exec.Command("go", "vet", "./...")
	cmd.Dir = projectDir
	out, err := executeCommand(cmd)
	if err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}
	return nil
}

func goTest(projectDir string) error {
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = projectDir
	out, err := executeCommand(cmd)
	if err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}
	return nil
}

func goBuild(projectDir string) error {
	cmd := exec.Command("go", "build", "-o", "tmp-app", "main.go")
	cmd.Dir = projectDir
	out, err := executeCommand(cmd)
	if err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}

	cmd = exec.Command("rm", "./tmp-app")
	cmd.Dir = projectDir
	out, err = executeCommand(cmd)
	if err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}

	return nil
}

func getDependencyVersion(filePath, dependency string) string {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return VersionUnknown
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.Contains(line, dependency) {
			parts := strings.Fields(line)
			if len(parts) > 1 {
				return parts[1]
			}
			return VersionNotFound
		}
	}

	return VersionUnknown
}

func gitCommit(projectDir, dependency, targetVersion string) error {
	cmd := exec.Command("git", "add", "go.mod", "go.sum")
	cmd.Dir = projectDir
	out, err := executeCommand(cmd)
	if err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}

	commitMessage := fmt.Sprintf("Updated %s to version %s", dependency, targetVersion)
	cmd = exec.Command("git", "commit", "-m", commitMessage)
	cmd.Dir = projectDir
	out, err = executeCommand(cmd)
	if err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}
	return nil
}

func gitPush(projectDir string) error {
	cmd := exec.Command("git", "push")
	cmd.Dir = projectDir
	out, err := executeCommand(cmd)
	if err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}
	return nil
}

func currentGitBranch(projectDir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = projectDir
	out, err := executeCommand(cmd)
	if err != nil {
		return "", fmt.Errorf("%v: %s", err, out)
	}
	return strings.TrimSpace(out), err
}

func gitCheckoutMaster(projectDir string) error {
	cmd := exec.Command("git", "checkout", "master")
	cmd.Dir = projectDir
	out, err := executeCommand(cmd)
	if err != nil {
		return fmt.Errorf("%v: %s", err, out)
	}
	return nil
}

func executeCommand(cmd *exec.Cmd) (string, error) {
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func readInput(prompt string, args ...any) string {
	reader := bufio.NewReader(os.Stdin)

	// Prompt the user for input
	fmt.Println(chalk.Yellow.Color(">>> " + fmt.Sprintf(prompt, args...)))

	// Use the reader to read the input until the first occurrence of \n
	text, err := reader.ReadString('\n')
	if err != nil {
		fmt.Println("An error occurred:", err)
		return ""
	}

	// Remove \n from what the user actually wrote
	return strings.TrimSuffix(text, "\n")
}

func printIndentedInfo(app, format string, args ...any) {
	log.Info(fmt.Sprintf("%s: %s", app, fmt.Sprintf(format, args...)))
}

func printIndentedError(app, format string, args ...any) {
	log.Error(fmt.Sprintf("%s: %s", app, fmt.Sprintf(format, args...)))
}

func printIndentedWarning(app, format string, args ...any) {
	log.Warn(fmt.Sprintf("%s: %s", app, fmt.Sprintf(format, args...)))
}

func directoryHasFile(directoryPath, fileName string) bool {
	filePath := path.Join(directoryPath, fileName)

	// Use os.Stat to get the file info
	_, err := os.Stat(filePath)

	// If the error is nil, the file exists
	if os.IsNotExist(err) {
		return false
	}
	return true
}
